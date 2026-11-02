package com.openpeats.ui

import android.annotation.SuppressLint
import android.app.Activity
import android.app.StatusBarManager
import android.content.ComponentName
import android.content.Context
import android.content.ContextWrapper
import android.content.pm.ApplicationInfo
import android.content.Intent
import android.graphics.drawable.Icon
import android.os.Build
import android.os.PowerManager
import android.provider.Settings
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.height
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.IntentSenderRequest
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.LifecycleResumeEffect
import com.openpeats.DeviceViewModel
import com.openpeats.R
import com.openpeats.companion.CompanionLink
import com.openpeats.service.AncTileService

private tailrec fun Context.activity(): Activity? = when (this) {
    is Activity -> this
    is ContextWrapper -> baseContext.activity()
    else -> null
}

/** Reaching the earbuds from outside the app, and keeping the link alive. */
@Composable
fun IntegrationCard(address: String?, keepConnected: Boolean) {
    val context = LocalContext.current
    val watching by CompanionLink.watching(context).collectAsState()
    var companionError by remember { mutableStateOf<String?>(null) }
    var unrestricted by remember { mutableStateOf(context.ignoresBatteryLimits()) }

    LifecycleResumeEffect(Unit) {
        unrestricted = context.ignoresBatteryLimits()
        onPauseOrDispose { }
    }

    val confirm = rememberLauncherForActivityResult(
        ActivityResultContracts.StartIntentSenderForResult()
    ) { }

    Section(stringResource(R.string.section_integration)) {
        Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Hint(stringResource(R.string.integration_widgets_hint))

            // Android lists no widget from an app on an adopted SD card. The
            // manifest asks for internal storage, but an option for developers
            // overrides that, and an update stays where the app already is.
            if (context.applicationInfo.flags and ApplicationInfo.FLAG_EXTERNAL_STORAGE != 0) {
                Hint(stringResource(R.string.integration_sd_card_hint))
            }

            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
                OutlinedButton(onClick = { context.requestTile() }) {
                    Text(stringResource(R.string.action_add_tile))
                }
            }

            // Both of these only serve a link that is kept up.
            if (keepConnected && CompanionLink.available && address != null) {
                Hint(stringResource(R.string.integration_companion_hint))
                if (watching == address) {
                    OutlinedButton(onClick = {
                        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) CompanionLink.forget(context)
                    }) {
                        Text(stringResource(R.string.action_companion_off))
                    }
                } else {
                    OutlinedButton(onClick = {
                        val activity = context.activity()
                        if (activity != null && Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
                            companionError = null
                            CompanionLink.associate(
                                activity,
                                address,
                                onConfirm = { confirm.launch(IntentSenderRequest.Builder(it).build()) },
                                onResult = { companionError = it },
                            )
                        }
                    }) {
                        Text(stringResource(R.string.action_companion_on))
                    }
                }
                companionError?.let { Hint(stringResource(R.string.integration_companion_failed, it)) }
            }

            // With the system watching, it restarts the service by itself.
            if (keepConnected && !unrestricted && watching == null) {
                Hint(stringResource(R.string.integration_battery_hint))
                OutlinedButton(onClick = {
                    context.startActivity(Intent(Settings.ACTION_IGNORE_BATTERY_OPTIMIZATION_SETTINGS))
                }) {
                    Text(stringResource(R.string.action_battery_settings))
                }
            }
        }
    }
}

/** How long the link lasts. */
@Composable
fun ConnectionCard(vm: DeviceViewModel) {
    val keep by vm.keepConnected.collectAsState()
    Section(stringResource(R.string.section_connection)) {
        SegmentedRow(
            options = listOf(
                1 to stringResource(R.string.connection_permanent),
                0 to stringResource(R.string.connection_on_demand),
            ),
            selected = if (keep) 1 else 0,
            enabled = true,
            onSelect = { vm.setKeepConnected(it == 1) },
        )
        Hint(
            stringResource(
                if (keep) R.string.connection_permanent_hint else R.string.connection_on_demand_hint
            )
        )
    }
}

/**
 * What can be turned off, and where the rest is. A running foreground service
 * must have a notification, so only the system can hide that one.
 */
@Composable
fun NotificationsCard(vm: DeviceViewModel) {
    val context = LocalContext.current
    val alerts by vm.batteryAlerts.collectAsState()
    Section(stringResource(R.string.section_notifications)) {
        SwitchRow(stringResource(R.string.toggle_battery_alerts), alerts, true) { vm.setBatteryAlerts(it) }
        Spacer(Modifier.height(4.dp))
        Hint(stringResource(R.string.notifications_hint))
        Spacer(Modifier.height(8.dp))
        OutlinedButton(onClick = {
            context.startActivity(
                Intent(Settings.ACTION_APP_NOTIFICATION_SETTINGS)
                    .putExtra(Settings.EXTRA_APP_PACKAGE, context.packageName)
            )
        }) {
            Text(stringResource(R.string.action_notification_settings))
        }
    }
}

@Composable
private fun Hint(text: String) {
    Text(text, fontSize = 12.sp, color = MaterialTheme.colorScheme.onSurfaceVariant)
}

private fun Context.ignoresBatteryLimits(): Boolean =
    getSystemService(PowerManager::class.java).isIgnoringBatteryOptimizations(packageName)

@SuppressLint("NewApi") // guarded by the caller
private fun Context.requestTile() {
    getSystemService(StatusBarManager::class.java).requestAddTileService(
        ComponentName(this, AncTileService::class.java),
        getString(R.string.tile_label),
        Icon.createWithResource(this, R.drawable.ic_mode_anc),
        mainExecutor,
    ) { }
}
