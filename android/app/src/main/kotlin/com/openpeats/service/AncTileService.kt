package com.openpeats.service

import android.annotation.SuppressLint
import android.content.Intent
import android.graphics.drawable.Icon
import android.os.Build
import android.service.quicksettings.Tile
import android.service.quicksettings.TileService
import com.openpeats.MainActivity
import com.openpeats.OpenPeatsApp
import com.openpeats.R
import com.openpeats.earbuds
import com.openpeats.hasBluetoothPermission
import com.openpeats.protocol.AncMode
import com.openpeats.session.EarbudsCommand
import com.openpeats.session.EarbudsView
import com.openpeats.session.LinkStatus
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.launch

/** A quick settings tile going through the noise cancelling modes. */
class AncTileService : TileService() {

    private var scope: CoroutineScope? = null

    override fun onStartListening() {
        super.onStartListening()
        val s = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
        scope = s
        s.launch { earbuds.view.collect { draw(it) } }
    }

    override fun onStopListening() {
        scope?.cancel()
        scope = null
        super.onStopListening()
    }

    override fun onClick() {
        super.onClick()
        if (!hasBluetoothPermission() || !earbuds.handle(EarbudsCommand.CycleAncMode)) {
            openApp()
            return
        }
        // The documentation does not list a tile among what may start a
        // foreground service from the background. With a kept link the service
        // is already there; if it is not and the system refuses, the app can.
        if (!EarbudsService.tryStart(this)) openApp()
    }

    @SuppressLint("StartActivityAndCollapseDeprecated")
    private fun openApp() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
            startActivityAndCollapse(OpenPeatsApp.openAppIntent(this))
        } else {
            @Suppress("DEPRECATION")
            startActivityAndCollapse(
                Intent(this, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
            )
        }
    }

    private fun draw(view: EarbudsView) {
        val tile = qsTile ?: return
        val mode = view.shownMode
        val connected = view.link == LinkStatus.CONNECTED
        tile.state = if (connected && mode != null && mode != AncMode.OFF.toInt()) {
            Tile.STATE_ACTIVE
        } else {
            Tile.STATE_INACTIVE
        }
        tile.icon = Icon.createWithResource(
            this,
            when (mode?.toByte()) {
                AncMode.ON -> R.drawable.ic_mode_anc
                AncMode.TRANSPARENCY -> R.drawable.ic_mode_transparency
                else -> R.drawable.ic_mode_off
            },
        )
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            tile.subtitle = when {
                view.known == null -> getString(R.string.status_no_earbuds)
                connected -> getString(EarbudsService.modeLabel(mode ?: AncMode.OFF.toInt()))
                view.link == LinkStatus.CONNECTING -> getString(R.string.status_connecting)
                view.link == LinkStatus.WAITING -> getString(R.string.status_waiting_short)
                else -> getString(R.string.status_disconnected)
            }
        }
        tile.updateTile()
    }
}
