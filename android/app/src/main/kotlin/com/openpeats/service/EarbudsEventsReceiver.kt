package com.openpeats.service

import android.bluetooth.BluetoothDevice
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import androidx.core.content.IntentCompat
import com.openpeats.earbuds

/**
 * The system events a kept link depends on. Declared in the manifest, because
 * they also have to reach a process that is gone: the phone restarted, the app
 * was updated, or the earbuds just left their case.
 */
class EarbudsEventsReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        val session = context.earbuds
        when (intent.action) {
            BluetoothDevice.ACTION_ACL_CONNECTED, BluetoothDevice.ACTION_ACL_DISCONNECTED -> {
                val device = IntentCompat.getParcelableExtra(
                    intent,
                    BluetoothDevice.EXTRA_DEVICE,
                    BluetoothDevice::class.java,
                ) ?: return
                val up = intent.action == BluetoothDevice.ACTION_ACL_CONNECTED
                session.onAudioLink(device.address, up)
                if (up && session.wanted.value == device.address) EarbudsService.tryStart(context)
            }
            Intent.ACTION_BOOT_COMPLETED, Intent.ACTION_MY_PACKAGE_REPLACED ->
                if (session.wanted.value != null) EarbudsService.tryStart(context)
        }
    }
}
