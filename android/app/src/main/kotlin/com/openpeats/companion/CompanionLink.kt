package com.openpeats.companion

import android.annotation.SuppressLint
import android.app.Activity
import android.companion.AssociationInfo
import android.companion.AssociationRequest
import android.companion.BluetoothDeviceFilter
import android.companion.CompanionDeviceManager
import android.companion.CompanionDeviceService
import android.companion.DevicePresenceEvent
import android.companion.ObservingDevicePresenceRequest
import android.content.Context
import android.content.IntentSender
import android.os.Build
import androidx.annotation.RequiresApi
import com.openpeats.earbuds
import com.openpeats.service.EarbudsService
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import java.util.concurrent.Executor

/**
 * Lets the system watch for the earbuds on the app's behalf.
 *
 * Optional, and it takes a confirmation dialog. In exchange the system restarts
 * the app when the earbuds appear, even if it was killed, so the service need
 * not sit in the notifications while they are away.
 */
object CompanionLink {
    private const val PREFS = "companion"
    private const val KEY_ADDRESS = "address"
    private const val KEY_ID = "id"

    private val watching = MutableStateFlow<String?>(null)
    private var loaded = false

    /** The association API this relies on is the one from Android 13. */
    val available: Boolean get() = Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU

    /** The address the system watches for, null if none. */
    fun watching(context: Context): StateFlow<String?> {
        if (!loaded) {
            loaded = true
            watching.value = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
                .getString(KEY_ADDRESS, null)
        }
        return watching
    }

    /**
     * Asks the system to associate with the earbuds. [onConfirm] shows its
     * dialog; [onResult] gets null on success, or else the reason it gives.
     */
    @RequiresApi(Build.VERSION_CODES.TIRAMISU)
    @SuppressLint("MissingPermission")
    fun associate(
        activity: Activity,
        address: String,
        onConfirm: (IntentSender) -> Unit,
        onResult: (String?) -> Unit,
    ) {
        val request = AssociationRequest.Builder()
            .addDeviceFilter(BluetoothDeviceFilter.Builder().setAddress(address).build())
            .setSingleDevice(true)
            .build()
        val manager = activity.getSystemService(CompanionDeviceManager::class.java)
        val context = activity.applicationContext
        val main = Executor { activity.mainExecutor.execute(it) }
        manager.associate(request, main, object : CompanionDeviceManager.Callback() {
            override fun onAssociationPending(sender: IntentSender) = onConfirm(sender)

            override fun onAssociationCreated(info: AssociationInfo) {
                observe(manager, info, address)
                context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit()
                    .putString(KEY_ADDRESS, address)
                    .putInt(KEY_ID, info.id)
                    .apply()
                watching.value = address
                onResult(null)
            }

            override fun onFailure(error: CharSequence?) = onResult(error?.toString().orEmpty())
        })
    }

    @RequiresApi(Build.VERSION_CODES.TIRAMISU)
    @SuppressLint("MissingPermission")
    @Suppress("DEPRECATION")
    private fun observe(manager: CompanionDeviceManager, info: AssociationInfo, address: String) {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.BAKLAVA) {
            manager.startObservingDevicePresence(
                ObservingDevicePresenceRequest.Builder().setAssociationId(info.id).build()
            )
        } else {
            manager.startObservingDevicePresence(address)
        }
    }

    /** Undoes [associate]: the service stays up while waiting again. */
    @RequiresApi(Build.VERSION_CODES.TIRAMISU)
    fun forget(context: Context) {
        val prefs = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
        val id = prefs.getInt(KEY_ID, -1)
        if (id >= 0) context.getSystemService(CompanionDeviceManager::class.java).disassociate(id)
        prefs.edit().clear().apply()
        watching.value = null
    }
}

/** Woken by the system when the associated earbuds come and go. */
@RequiresApi(Build.VERSION_CODES.TIRAMISU)
class EarbudsCompanionService : CompanionDeviceService() {

    private fun presence(address: String?, up: Boolean) {
        if (address == null) return
        earbuds.onAudioLink(address, up)
        if (up && earbuds.wanted.value == address) EarbudsService.tryStart(this)
    }

    @Deprecated("Replaced from Android 16 by onDevicePresenceEvent")
    @Suppress("DEPRECATION")
    override fun onDeviceAppeared(info: AssociationInfo) =
        presence(info.deviceMacAddress?.toString()?.uppercase(), true)

    @Deprecated("Replaced from Android 16 by onDevicePresenceEvent")
    @Suppress("DEPRECATION")
    override fun onDeviceDisappeared(info: AssociationInfo) =
        presence(info.deviceMacAddress?.toString()?.uppercase(), false)

    @RequiresApi(Build.VERSION_CODES.BAKLAVA)
    override fun onDevicePresenceEvent(event: DevicePresenceEvent) {
        val manager = getSystemService(CompanionDeviceManager::class.java)
        val info = manager.myAssociations.firstOrNull { it.id == event.associationId } ?: return
        val address = info.deviceMacAddress?.toString()?.uppercase()
        when (event.event) {
            DevicePresenceEvent.EVENT_BT_CONNECTED,
            DevicePresenceEvent.EVENT_BLE_APPEARED -> presence(address, true)
            DevicePresenceEvent.EVENT_BT_DISCONNECTED,
            DevicePresenceEvent.EVENT_BLE_DISAPPEARED -> presence(address, false)
        }
    }
}
