package com.openpeats.session

import android.annotation.SuppressLint
import android.bluetooth.BluetoothAdapter
import android.bluetooth.BluetoothManager
import android.bluetooth.BluetoothProfile
import android.content.Context
import android.os.Build
import com.openpeats.transport.RfcommClient
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.coroutines.withTimeoutOrNull
import kotlin.coroutines.resume

/** The session's platform side: resources and the Bluetooth adapter. */
class AndroidEarbudsEnv(private val context: Context) : EarbudsEnv {

    private fun adapter(): BluetoothAdapter? =
        (context.getSystemService(Context.BLUETOOTH_SERVICE) as? BluetoothManager)?.adapter

    override fun text(id: Int, vararg args: Any): String = context.getString(id, *args)

    override fun quantity(id: Int, count: Int, vararg args: Any): String =
        context.resources.getQuantityString(id, count, *args)

    @SuppressLint("MissingPermission")
    override fun deviceName(address: String): String? = try {
        adapter()?.getRemoteDevice(address)?.name
    } catch (_: SecurityException) {
        null
    }

    override suspend fun openLink(address: String, onLost: (RfcommClient) -> Unit): RfcommClient {
        val device = adapter()?.getRemoteDevice(address) ?: throw NoBluetoothException()
        return RfcommClient.connect(device, onLost)
    }

    /**
     * Asks the audio profiles, the only public way of telling whether the phone
     * is linked to a device. Each answers through a callback once bound.
     */
    override suspend fun audioLinkUp(address: String): Boolean? {
        val adapter = adapter() ?: return null
        val profiles = buildList {
            add(BluetoothProfile.A2DP)
            add(BluetoothProfile.HEADSET)
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) add(BluetoothProfile.LE_AUDIO)
        }
        val answers = profiles.map { connectedTo(adapter, it, address) }
        return when {
            answers.any { it == true } -> true
            answers.all { it == null } -> null
            else -> false
        }
    }

    @SuppressLint("MissingPermission")
    private suspend fun connectedTo(adapter: BluetoothAdapter, profile: Int, address: String): Boolean? =
        withTimeoutOrNull(PROFILE_TIMEOUT_MS) {
            suspendCancellableCoroutine { cont ->
                val listener = object : BluetoothProfile.ServiceListener {
                    override fun onServiceConnected(p: Int, proxy: BluetoothProfile) {
                        val up = try {
                            proxy.connectedDevices.any { it.address == address }
                        } catch (_: SecurityException) {
                            null
                        }
                        adapter.closeProfileProxy(p, proxy)
                        if (cont.isActive) cont.resume(up)
                    }

                    override fun onServiceDisconnected(p: Int) {}
                }
                if (!adapter.getProfileProxy(context, listener, profile)) cont.resume(null)
            }
        }

    override fun now(): Long = System.currentTimeMillis()

    private companion object {
        const val PROFILE_TIMEOUT_MS = 2000L
    }
}

/** Keeps the session's memory in the app's preferences. */
class PrefsEarbudsStore(context: Context) : EarbudsStore {
    private val prefs = context.getSharedPreferences("earbuds", Context.MODE_PRIVATE)

    override var wanted: String?
        get() = prefs.getString("wanted", null)
        set(value) = prefs.edit().putString("wanted", value).apply()

    override var known: KnownEarbuds?
        get() = KnownEarbuds.fromJson(prefs.getString("known", null))
        set(value) = prefs.edit().putString("known", value?.toJson()).apply()

    override var keepConnected: Boolean
        get() = prefs.getBoolean("keepConnected", true)
        set(value) = prefs.edit().putBoolean("keepConnected", value).apply()

    override var batteryAlerts: Boolean
        get() = prefs.getBoolean("batteryAlerts", true)
        set(value) = prefs.edit().putBoolean("batteryAlerts", value).apply()
}
