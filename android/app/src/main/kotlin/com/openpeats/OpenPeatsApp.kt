package com.openpeats

import android.Manifest
import android.annotation.SuppressLint
import android.app.Application
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.content.res.Configuration
import android.os.Build
import androidx.annotation.StringRes
import androidx.core.app.NotificationCompat
import androidx.core.content.ContextCompat
import com.openpeats.appwidget.updateAllWidgets
import com.openpeats.companion.CompanionLink
import com.openpeats.service.EarbudsService
import com.openpeats.session.AndroidEarbudsEnv
import com.openpeats.session.EarbudsSession
import com.openpeats.session.LinkStatus
import com.openpeats.session.LowBattery
import com.openpeats.session.PrefsEarbudsStore
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.FlowPreview
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.debounce
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch

/** Owns the one link to the earbuds and what follows its state. */
class OpenPeatsApp : Application() {

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)

    lateinit var earbuds: EarbudsSession
        private set

    /**
     * Whether the foreground service should be running: for as long as a link
     * is kept up. The exception is waiting for earbuds the system watches for
     * us, where it can restart the service when they appear, so no notification
     * has to sit there meanwhile.
     */
    lateinit var keepService: StateFlow<Boolean>
        private set

    @OptIn(FlowPreview::class)
    override fun onCreate() {
        super.onCreate()
        earbuds = EarbudsSession(AndroidEarbudsEnv(this), PrefsEarbudsStore(this), scope)
        createNotificationChannels()

        val companion = CompanionLink.watching(this)
        keepService = combine(earbuds.wanted, earbuds.view, companion) { wanted, view, watching ->
            wanted != null && !(view.link == LinkStatus.WAITING && watching == wanted)
        }.stateIn(scope, SharingStarted.Eagerly, earbuds.wanted.value != null)

        scope.launch {
            keepService.collect { keep -> if (keep) EarbudsService.tryStart(this@OpenPeatsApp) }
        }
        scope.launch {
            // Several fields often change together: one redraw covers them.
            earbuds.view.debounce(WIDGET_DEBOUNCE_MS).collect { updateAllWidgets(this@OpenPeatsApp) }
        }
        scope.launch {
            earbuds.lowBattery.collect { notifyLowBattery(it) }
        }
    }

    /** The language changed, from the system or the app: redraw what is outside the app. */
    override fun onConfigurationChanged(newConfig: Configuration) {
        super.onConfigurationChanged(newConfig)
        createNotificationChannels()
        scope.launch { updateAllWidgets(this@OpenPeatsApp) }
    }

    private fun createNotificationChannels() {
        val manager = getSystemService(NotificationManager::class.java)
        manager.createNotificationChannel(
            NotificationChannel(
                CHANNEL_LINK,
                getString(R.string.channel_link),
                NotificationManager.IMPORTANCE_LOW,
            ).apply { setShowBadge(false) }
        )
        manager.createNotificationChannel(
            NotificationChannel(
                CHANNEL_ALERTS,
                getString(R.string.channel_alerts),
                NotificationManager.IMPORTANCE_DEFAULT,
            )
        )
    }

    @SuppressLint("MissingPermission")
    private fun notifyLowBattery(low: LowBattery) {
        if (!canNotify()) return
        val text = getString(R.string.notification_low_battery, getString(batteryLabel(low.source)), low.level)
        val notification = NotificationCompat.Builder(this, CHANNEL_ALERTS)
            .setSmallIcon(R.drawable.ic_stat_openpeats)
            .setContentTitle(earbuds.known.value?.name ?: getString(R.string.app_name))
            .setContentText(text)
            .setContentIntent(openAppIntent(this))
            .setAutoCancel(true)
            .build()
        getSystemService(NotificationManager::class.java)
            .notify(low.source, NOTIFICATION_LOW_BATTERY, notification)
    }

    private fun canNotify(): Boolean =
        Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU ||
            ContextCompat.checkSelfPermission(this, Manifest.permission.POST_NOTIFICATIONS) ==
            PackageManager.PERMISSION_GRANTED

    @StringRes
    private fun batteryLabel(source: String): Int = when (source) {
        "batteryLeft" -> R.string.feature_battery_left
        "batteryRight" -> R.string.feature_battery_right
        else -> R.string.feature_battery_case
    }

    companion object {
        const val CHANNEL_LINK = "link"
        const val CHANNEL_ALERTS = "alerts"
        const val NOTIFICATION_LINK = 1
        const val NOTIFICATION_LOW_BATTERY = 2
        const val NOTIFICATION_PERMISSION = 3

        private const val WIDGET_DEBOUNCE_MS = 200L

        fun openAppIntent(context: Context): PendingIntent = PendingIntent.getActivity(
            context,
            0,
            Intent(context, MainActivity::class.java)
                .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_SINGLE_TOP),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )
    }
}

/** The app's session, from anything holding a context. */
val Context.earbuds: EarbudsSession
    get() = (applicationContext as OpenPeatsApp).earbuds

/**
 * The one runtime permission talking to earbuds takes, from Android 12 on: the
 * app only uses paired devices and never scans.
 */
fun Context.hasBluetoothPermission(): Boolean =
    Build.VERSION.SDK_INT < Build.VERSION_CODES.S ||
        ContextCompat.checkSelfPermission(this, Manifest.permission.BLUETOOTH_CONNECT) ==
        PackageManager.PERMISSION_GRANTED
