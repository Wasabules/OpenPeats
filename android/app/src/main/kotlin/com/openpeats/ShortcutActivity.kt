package com.openpeats

import android.app.Activity
import android.content.Intent
import android.os.Bundle
import com.openpeats.service.EarbudsService

/**
 * Where a launcher shortcut lands: passes its command to the service and
 * disappears, with no screen. Shortcuts have to target an activity, and being
 * one is what allows the service to start. Not exported, so no other app can
 * change the mode through it.
 */
class ShortcutActivity : Activity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val started = hasBluetoothPermission() && earbuds.known.value != null &&
            EarbudsService.commandFrom(intent) != null &&
            EarbudsService.tryStart(this, Intent(this, EarbudsService::class.java).setAction(intent.action))
        if (!started) startActivity(Intent(this, MainActivity::class.java))
        finish()
    }
}
