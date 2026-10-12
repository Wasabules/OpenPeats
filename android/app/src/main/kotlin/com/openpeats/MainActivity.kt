package com.openpeats

import android.Manifest
import android.annotation.SuppressLint
import android.app.Activity
import android.content.Context
import android.content.ContextWrapper
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.provider.Settings
import androidx.activity.ComponentActivity
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.compose.setContent
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.core.app.ActivityCompat
import androidx.core.content.ContextCompat
import androidx.lifecycle.compose.LifecycleResumeEffect
import androidx.lifecycle.viewmodel.compose.viewModel
import com.openpeats.ui.ConnectedView
import com.openpeats.ui.DevicePicker

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContent {
            MaterialTheme(colorScheme = darkColorScheme()) {
                Surface(Modifier.fillMaxSize()) { App() }
            }
        }
    }
}

/**
 * The one runtime permission the app needs, from Android 12 on. Talking to
 * paired earbuds takes CONNECT only: the app never scans, so it does not ask
 * for SCAN. Only ever requested from Android 12 on: below that,
 * [hasBluetoothPermission] is true and the request never fires.
 */
@SuppressLint("InlinedApi")
private const val BT_PERMISSION = Manifest.permission.BLUETOOTH_CONNECT

private fun Context.hasBluetoothPermission(): Boolean =
    Build.VERSION.SDK_INT < Build.VERSION_CODES.S ||
        ContextCompat.checkSelfPermission(this, BT_PERMISSION) == PackageManager.PERMISSION_GRANTED

private tailrec fun Context.findActivity(): Activity? = when (this) {
    is Activity -> this
    is ContextWrapper -> baseContext.findActivity()
    else -> null
}

@Composable
fun App(vm: DeviceViewModel = viewModel()) {
    val state by vm.state.collectAsState()
    val log by vm.log.collectAsState()
    val context = LocalContext.current

    // Read from the system rather than assumed: a permission granted on a
    // previous run must not show the "permission required" card again.
    var granted by remember { mutableStateOf(context.hasBluetoothPermission()) }
    var deniedForever by remember { mutableStateOf(false) }
    val launcher = rememberLauncherForActivityResult(
        ActivityResultContracts.RequestPermission()
    ) { ok ->
        granted = ok
        // After a refusal, no rationale left to show means the system will not
        // ask again: only the app settings can grant it from now on.
        val activity = context.findActivity()
        deniedForever = !ok && activity != null &&
            !ActivityCompat.shouldShowRequestPermissionRationale(activity, BT_PERMISSION)
    }

    LaunchedEffect(Unit) {
        if (!granted) launcher.launch(BT_PERMISSION)
    }

    // Coming back from the settings screen, where the permission may just
    // have been granted.
    LifecycleResumeEffect(Unit) {
        if (!granted && context.hasBluetoothPermission()) {
            granted = true
            deniedForever = false
        }
        onPauseOrDispose { }
    }

    // targetSdk 35 forces edge-to-edge display: without this inset the content
    // slides under the status bar and under the navigation buttons.
    Column(
        Modifier
            .fillMaxSize()
            .windowInsetsPadding(WindowInsets.safeDrawing)
            .verticalScroll(rememberScrollState())
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(14.dp),
    ) {
        Text(
            stringResource(R.string.app_name),
            fontSize = 26.sp,
            fontWeight = FontWeight.Bold,
        )

        if (!granted) {
            PermissionCard(
                deniedForever = deniedForever,
                onRequest = { launcher.launch(BT_PERMISSION) },
                onOpenSettings = {
                    context.startActivity(
                        Intent(
                            Settings.ACTION_APPLICATION_DETAILS_SETTINGS,
                            Uri.fromParts("package", context.packageName, null),
                        )
                    )
                },
            )
            return@Column
        }

        state.error?.let { msg ->
            Card(
                Modifier.fillMaxWidth(),
                colors = CardDefaults.cardColors(containerColor = Color(0xFF4A1F1F)),
            ) {
                Text(msg, Modifier.padding(14.dp), fontSize = 14.sp)
            }
        }

        if (!state.connected) {
            DevicePicker(vm, state)
        } else {
            ConnectedView(vm, state)
        }

        if (log.isNotEmpty()) {
            Card(Modifier.fillMaxWidth()) {
                Column(Modifier.padding(14.dp)) {
                    Text(
                        stringResource(R.string.section_log),
                        fontWeight = FontWeight.Bold,
                        fontSize = 15.sp,
                    )
                    Spacer(Modifier.height(6.dp))
                    log.takeLast(8).forEach {
                        Text(it, fontSize = 12.sp, fontFamily = FontFamily.Monospace)
                    }
                }
            }
        }
    }
}

/**
 * Shown until the permission is granted. Never a dead end: the request can be
 * made again, and once the system stops asking, the settings screen is one
 * tap away.
 */
@Composable
private fun PermissionCard(
    deniedForever: Boolean,
    onRequest: () -> Unit,
    onOpenSettings: () -> Unit,
) {
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            Text(stringResource(R.string.permission_required))
            if (deniedForever) {
                Text(
                    stringResource(R.string.permission_denied_forever),
                    fontSize = 13.sp,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                Button(onClick = onOpenSettings) {
                    Text(stringResource(R.string.action_open_settings))
                }
            } else {
                Button(onClick = onRequest) {
                    Text(stringResource(R.string.action_grant))
                }
            }
        }
    }
}
