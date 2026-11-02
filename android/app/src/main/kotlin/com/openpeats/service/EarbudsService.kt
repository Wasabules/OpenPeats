package com.openpeats.service

import android.annotation.SuppressLint
import android.app.Notification
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.content.res.Configuration
import android.os.Build
import android.os.IBinder
import android.util.Log
import androidx.annotation.StringRes
import androidx.core.app.NotificationCompat
import androidx.core.app.ServiceCompat
import androidx.core.content.ContextCompat
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
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.launch

private const val TAG = "OpenPeats"

/**
 * Keeps the process, and so the link, alive with no screen open. The link
 * itself lives in the session: this adds the notification the system requires,
 * and starting it is how a widget reaches the session from a cold process.
 */
class EarbudsService : Service() {

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private var watching = false

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onCreate() {
        super.onCreate()
        running = true
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        // Android allows 5 seconds to get here, whatever the command turns out
        // to be. Nothing may return before it.
        if (!promote()) {
            stopSelf(startId)
            return START_NOT_STICKY
        }

        if (!hasBluetoothPermission()) {
            // Reached from a widget drawn before the permission was revoked.
            notifyPermissionRequired()
            ServiceCompat.stopForeground(this, ServiceCompat.STOP_FOREGROUND_REMOVE)
            stopSelf(startId)
            return START_NOT_STICKY
        }

        when (intent?.action) {
            ACTION_STOP -> earbuds.stop()
            else -> {
                commandFrom(intent)?.let { earbuds.handle(it) }
                earbuds.resume()
            }
        }

        // Only now: before the command above runs, keepService can still read
        // false for a tap that is about to set it.
        if (!watching) {
            watching = true
            val app = application as OpenPeatsApp
            scope.launch { earbuds.view.collect { show(it) } }
            scope.launch {
                app.keepService.collect { keep -> if (!keep) shutDown() }
            }
        }
        return START_STICKY
    }

    override fun onConfigurationChanged(newConfig: Configuration) {
        super.onConfigurationChanged(newConfig)
        // The notification was written in the previous language.
        if (watching) show(earbuds.view.value)
    }

    private fun shutDown() {
        // Before anything else: a redraw arriving after this point would post
        // the notification again, this time with no service to remove it.
        scope.cancel()
        running = false
        ServiceCompat.stopForeground(this, ServiceCompat.STOP_FOREGROUND_REMOVE)
        stopSelf()
    }

    override fun onDestroy() {
        running = false
        scope.cancel()
        super.onDestroy()
    }

    /**
     * Enters the foreground, false if the system refuses: on Android 12 and
     * later it can, when a restart comes with no user action behind it.
     *
     * From Android 14 the connectedDevice type needs the Bluetooth permission
     * granted. The short type asks for nothing, which is what lets the service
     * meet its 5 seconds and report a revoked permission instead of crashing.
     */
    @SuppressLint("InlinedApi") // ServiceCompat drops the type where it does not exist
    private fun promote(): Boolean {
        val type = when {
            hasBluetoothPermission() || Build.VERSION.SDK_INT < Build.VERSION_CODES.UPSIDE_DOWN_CAKE ->
                ServiceInfo.FOREGROUND_SERVICE_TYPE_CONNECTED_DEVICE
            else -> ServiceInfo.FOREGROUND_SERVICE_TYPE_SHORT_SERVICE
        }
        return try {
            ServiceCompat.startForeground(this, OpenPeatsApp.NOTIFICATION_LINK, build(earbuds.view.value), type)
            true
        } catch (e: IllegalStateException) {
            // ForegroundServiceStartNotAllowedException, from API 31.
            Log.w(TAG, "foreground refused", e)
            false
        }
    }

    private fun show(view: EarbudsView) {
        getSystemService(NotificationManager::class.java)
            .notify(OpenPeatsApp.NOTIFICATION_LINK, build(view))
    }

    private fun build(view: EarbudsView): Notification {
        val known = view.known
        val builder = NotificationCompat.Builder(this, OpenPeatsApp.CHANNEL_LINK)
            .setSmallIcon(R.drawable.ic_stat_openpeats)
            .setContentTitle(known?.name ?: getString(R.string.app_name))
            .setContentIntent(OpenPeatsApp.openAppIntent(this))
            .setOngoing(true)
            .setOnlyAlertOnce(true)
            .setSilent(true)
            .setCategory(NotificationCompat.CATEGORY_SERVICE)
            .setForegroundServiceBehavior(NotificationCompat.FOREGROUND_SERVICE_IMMEDIATE)

        when (view.link) {
            LinkStatus.CONNECTED -> {
                builder.setContentText(connectedText(view))
                if (known != null && "ancMode" !in known.unsupported) {
                    val shown = view.shownMode
                    MODES.forEach { (mode, label) ->
                        val text = if (mode == shown) getString(R.string.notification_action_active, getString(label))
                        else getString(label)
                        builder.addAction(0, text, modeIntent(mode))
                    }
                }
            }
            LinkStatus.CONNECTING -> builder
                .setContentText(getString(R.string.status_connecting))
                .addAction(0, getString(R.string.action_stop), stopIntent())
            else -> builder
                .setContentText(getString(R.string.status_waiting))
                .addAction(0, getString(R.string.action_stop), stopIntent())
        }
        return builder.build()
    }

    private fun connectedText(view: EarbudsView): String {
        val k = view.known ?: return getString(R.string.status_connected)
        val parts = buildList {
            if ("ancMode" !in k.unsupported) add(getString(modeLabel(view.shownMode ?: k.ancMode)))
            if ("batteryLeft" !in k.unsupported) add(getString(R.string.notification_left, k.batteryLeft))
            if ("batteryRight" !in k.unsupported) add(getString(R.string.notification_right, k.batteryRight))
            if ("batteryCase" !in k.unsupported) add(getString(R.string.notification_case, k.batteryCase))
        }
        return parts.joinToString(" · ")
    }

    private fun modeIntent(mode: Int): PendingIntent = PendingIntent.getForegroundService(
        this,
        REQUEST_MODE + mode,
        intentFor(this, EarbudsCommand.AncModeTo(mode)),
        PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
    )

    private fun stopIntent(): PendingIntent = PendingIntent.getForegroundService(
        this,
        REQUEST_STOP,
        Intent(this, EarbudsService::class.java).setAction(ACTION_STOP),
        PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
    )

    private fun notifyPermissionRequired() {
        val notification = NotificationCompat.Builder(this, OpenPeatsApp.CHANNEL_ALERTS)
            .setSmallIcon(R.drawable.ic_stat_openpeats)
            .setContentTitle(getString(R.string.app_name))
            .setContentText(getString(R.string.permission_required))
            .setContentIntent(OpenPeatsApp.openAppIntent(this))
            .setAutoCancel(true)
            .build()
        getSystemService(NotificationManager::class.java)
            .notify(OpenPeatsApp.NOTIFICATION_PERMISSION, notification)
    }

    companion object {
        private const val ACTION_STOP = "com.openpeats.action.STOP"
        const val ACTION_MODE_OFF = "com.openpeats.action.MODE_OFF"
        const val ACTION_MODE_ANC = "com.openpeats.action.MODE_ANC"
        const val ACTION_MODE_TRANSPARENCY = "com.openpeats.action.MODE_TRANSPARENCY"
        private const val ACTION_MODE_OTHER = "com.openpeats.action.MODE"
        private const val ACTION_CYCLE = "com.openpeats.action.CYCLE"
        private const val ACTION_ANC_LEVEL = "com.openpeats.action.ANC_LEVEL"
        private const val ACTION_TRANSPARENCY_LEVEL = "com.openpeats.action.TRANSPARENCY_LEVEL"
        private const val ACTION_TOGGLE_GAME = "com.openpeats.action.TOGGLE_GAME"
        private const val ACTION_TOGGLE_TOUCH = "com.openpeats.action.TOGGLE_TOUCH"
        private const val EXTRA_VALUE = "value"

        private const val REQUEST_STOP = 1
        private const val REQUEST_MODE = 100

        private val MODES = listOf(
            AncMode.OFF.toInt() to R.string.anc_off,
            AncMode.ON.toInt() to R.string.anc_on,
            AncMode.TRANSPARENCY.toInt() to R.string.anc_transparency,
        )

        /** Whether the service exists in this process, which is what starting it would achieve. */
        @Volatile
        var running = false
            private set

        @StringRes
        fun modeLabel(mode: Int): Int = when (mode.toByte()) {
            AncMode.ON -> R.string.anc_on
            AncMode.TRANSPARENCY -> R.string.anc_transparency
            AncMode.ADAPTIVE -> R.string.anc_level_adaptive
            else -> R.string.anc_off
        }

        /**
         * Starts the service if it can be. The system refuses a start from the
         * background unless a user action or an exemption is behind it, so
         * false is an expected answer, not an error.
         */
        fun tryStart(context: Context, intent: Intent? = null): Boolean {
            if (!context.hasBluetoothPermission()) return false
            if (running && intent == null) return true
            return try {
                ContextCompat.startForegroundService(
                    context,
                    intent ?: Intent(context, EarbudsService::class.java),
                )
                true
            } catch (e: IllegalStateException) {
                Log.i(TAG, "service not started from here", e)
                false
            }
        }

        /** The service intent that runs [command]. */
        fun intentFor(context: Context, command: EarbudsCommand): Intent {
            val intent = Intent(context, EarbudsService::class.java)
            return when (command) {
                is EarbudsCommand.AncModeTo -> when (command.mode.toByte()) {
                    AncMode.OFF -> intent.setAction(ACTION_MODE_OFF)
                    AncMode.ON -> intent.setAction(ACTION_MODE_ANC)
                    AncMode.TRANSPARENCY -> intent.setAction(ACTION_MODE_TRANSPARENCY)
                    else -> intent.setAction(ACTION_MODE_OTHER).putExtra(EXTRA_VALUE, command.mode)
                }
                EarbudsCommand.CycleAncMode -> intent.setAction(ACTION_CYCLE)
                is EarbudsCommand.AncLevelTo ->
                    intent.setAction(ACTION_ANC_LEVEL).putExtra(EXTRA_VALUE, command.code)
                is EarbudsCommand.TransparencyLevelTo ->
                    intent.setAction(ACTION_TRANSPARENCY_LEVEL).putExtra(EXTRA_VALUE, command.code)
                EarbudsCommand.ToggleGameMode -> intent.setAction(ACTION_TOGGLE_GAME)
                EarbudsCommand.ToggleTouchLock -> intent.setAction(ACTION_TOGGLE_TOUCH)
            }
        }

        /** The command an intent carries, null if it carries none. */
        fun commandFrom(intent: Intent?): EarbudsCommand? {
            val value = intent?.getIntExtra(EXTRA_VALUE, -1) ?: -1
            return when (intent?.action) {
                ACTION_MODE_OFF -> EarbudsCommand.AncModeTo(AncMode.OFF.toInt())
                ACTION_MODE_ANC -> EarbudsCommand.AncModeTo(AncMode.ON.toInt())
                ACTION_MODE_TRANSPARENCY -> EarbudsCommand.AncModeTo(AncMode.TRANSPARENCY.toInt())
                ACTION_MODE_OTHER -> if (value >= 0) EarbudsCommand.AncModeTo(value) else null
                ACTION_CYCLE -> EarbudsCommand.CycleAncMode
                ACTION_ANC_LEVEL -> if (value >= 0) EarbudsCommand.AncLevelTo(value) else null
                ACTION_TRANSPARENCY_LEVEL -> if (value >= 0) EarbudsCommand.TransparencyLevelTo(value) else null
                ACTION_TOGGLE_GAME -> EarbudsCommand.ToggleGameMode
                ACTION_TOGGLE_TOUCH -> EarbudsCommand.ToggleTouchLock
                else -> null
            }
        }
    }
}
