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
import androidx.annotation.StringRes
import androidx.activity.ComponentActivity
import androidx.activity.compose.BackHandler
import androidx.activity.SystemBarStyle
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.GraphicEq
import androidx.compose.material.icons.filled.Headphones
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.filled.Tune
import androidx.compose.material.icons.filled.Widgets
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.core.app.ActivityCompat
import androidx.core.content.ContextCompat
import androidx.lifecycle.compose.LifecycleResumeEffect
import androidx.lifecycle.compose.LifecycleStartEffect
import androidx.lifecycle.viewmodel.compose.viewModel
import com.openpeats.protocol.EqBand
import com.openpeats.ui.ConnectionCard
import com.openpeats.ui.DevicePicker
import com.openpeats.ui.EarbudsControls
import com.openpeats.ui.EarbudsSettings
import com.openpeats.ui.EqPanel
import com.openpeats.ui.IntegrationCard
import com.openpeats.ui.LanguageCard
import com.openpeats.ui.LastRead
import com.openpeats.ui.NotificationsCard
import com.openpeats.ui.Section

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        // Bar icons follow the theme. Android 15 forces edge-to-edge anyway;
        // this makes older versions match, and the insets below handle both.
        enableEdgeToEdge(
            statusBarStyle = SystemBarStyle.auto(
                android.graphics.Color.TRANSPARENT,
                android.graphics.Color.TRANSPARENT,
            ),
        )
        super.onCreate(savedInstanceState)
        setContent {
            MaterialTheme(colorScheme = appColors()) {
                Surface(Modifier.fillMaxSize()) { App() }
            }
        }
    }
}

/** The wallpaper's colours where the system offers them, its light or dark mode always. */
@Composable
private fun appColors(): ColorScheme {
    val dark = isSystemInDarkTheme()
    val context = LocalContext.current
    return when {
        Build.VERSION.SDK_INT >= Build.VERSION_CODES.S ->
            if (dark) dynamicDarkColorScheme(context) else dynamicLightColorScheme(context)
        dark -> darkColorScheme()
        else -> lightColorScheme()
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

private tailrec fun Context.findActivity(): Activity? = when (this) {
    is Activity -> this
    is ContextWrapper -> baseContext.findActivity()
    else -> null
}

@Composable
fun App(vm: DeviceViewModel = viewModel()) {
    val wanted by vm.wanted.collectAsState()
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

    // A kept link comes back with the app, should the process have died.
    LaunchedEffect(granted) {
        if (granted) vm.resume()
    }

    // Asked for once a link is kept, which is when the notification appears
    // and can say why. Refusing only hides it: the link works the same.
    val notifications = rememberLauncherForActivityResult(
        ActivityResultContracts.RequestPermission()
    ) { }
    LaunchedEffect(wanted != null) {
        if (wanted != null && Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU &&
            ContextCompat.checkSelfPermission(context, Manifest.permission.POST_NOTIFICATIONS) !=
            PackageManager.PERMISSION_GRANTED
        ) {
            notifications.launch(Manifest.permission.POST_NOTIFICATIONS)
        }
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

    if (granted) {
        MainScreen(vm)
        return
    }
    Column(
        Modifier
            .fillMaxSize()
            .windowInsetsPadding(WindowInsets.safeDrawing)
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(14.dp),
    ) {
        Text(stringResource(R.string.app_name), fontSize = 26.sp, fontWeight = FontWeight.Bold)
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
    }
}

private enum class Tab(@StringRes val label: Int, val icon: ImageVector) {
    EARBUDS(R.string.tab_earbuds, Icons.Filled.Headphones),
    SOUND(R.string.tab_sound, Icons.Filled.GraphicEq),
    SETTINGS(R.string.tab_settings, Icons.Filled.Tune),
    APP(R.string.tab_app, Icons.Filled.Widgets),
}

/** One tab per reason to open the app, each scrolling on its own. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun MainScreen(vm: DeviceViewModel) {
    val state by vm.state.collectAsState()
    val log by vm.log.collectAsState()
    val known by vm.known.collectAsState()
    val wanted by vm.wanted.collectAsState()
    val keepConnected by vm.keepConnected.collectAsState()

    // A link kept only for use is in use while the app is on screen.
    LifecycleStartEffect(Unit) {
        vm.setScreenOpen(true)
        onStopOrDispose { vm.setScreenOpen(false) }
    }

    var tab by rememberSaveable { mutableStateOf(Tab.EARBUDS) }
    BackHandler(tab != Tab.EARBUDS) { tab = Tab.EARBUDS }
    val scrolls = Tab.entries.associateWith { rememberScrollState() }

    Scaffold(
        topBar = {
            TopAppBar(
                title = {
                    Text(
                        if (tab == Tab.EARBUDS) state.deviceName.ifEmpty { stringResource(R.string.app_name) }
                        else stringResource(tab.label),
                        maxLines = 1,
                    )
                },
                actions = {
                    if (tab == Tab.EARBUDS && state.connected) {
                        IconButton(onClick = { vm.refresh() }, enabled = !state.busy) {
                            Icon(Icons.Filled.Refresh, stringResource(R.string.action_refresh))
                        }
                        TextButton(onClick = { vm.disconnect() }) {
                            Text(stringResource(R.string.action_close))
                        }
                    }
                },
            )
        },
        bottomBar = {
            NavigationBar {
                Tab.entries.forEach { t ->
                    NavigationBarItem(
                        selected = t == tab,
                        onClick = { tab = t },
                        icon = { Icon(t.icon, null) },
                        label = { Text(stringResource(t.label), maxLines = 1) },
                    )
                }
            }
        },
    ) { padding ->
        Column(
            Modifier
                .fillMaxSize()
                .padding(padding)
                .verticalScroll(scrolls.getValue(tab))
                .padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(14.dp),
        ) {
            when (tab) {
                Tab.EARBUDS -> {
                    state.error?.let { ErrorCard(it) }
                    if (state.connected) {
                        EarbudsControls(vm, state)
                    } else {
                        DevicePicker(vm, state)
                        known?.takeIf { it.readAtMs > 0 }?.let { LastRead(it) }
                    }
                }
                Tab.SOUND ->
                    if (state.connected) {
                        Section(stringResource(R.string.section_equaliser)) {
                            EqPanel(vm.eqEditor, busy = state.busy) { bands: List<EqBand>, master: Double ->
                                vm.applyEq(bands, master)
                            }
                        }
                    } else {
                        NeedsLink { tab = Tab.EARBUDS }
                    }
                Tab.SETTINGS ->
                    if (state.connected) {
                        EarbudsSettings(vm, state, wanted)
                    } else {
                        NeedsLink { tab = Tab.EARBUDS }
                    }
                Tab.APP -> {
                    ConnectionCard(vm)
                    NotificationsCard(vm)
                    IntegrationCard(wanted ?: known?.address, keepConnected)
                    LanguageCard()
                    LogCard(log)
                    AboutLine()
                }
            }
        }
    }
}

@Composable
private fun ErrorCard(message: String) {
    Card(
        Modifier.fillMaxWidth(),
        colors = CardDefaults.cardColors(
            containerColor = MaterialTheme.colorScheme.errorContainer,
            contentColor = MaterialTheme.colorScheme.onErrorContainer,
        ),
    ) {
        Text(message, Modifier.padding(14.dp), fontSize = 14.sp)
    }
}

/** Where a tab that needs the earbuds stands in for its content. */
@Composable
private fun NeedsLink(onGo: () -> Unit) {
    Column(
        Modifier
            .fillMaxWidth()
            .padding(top = 48.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Icon(
            Icons.Filled.Headphones,
            null,
            Modifier.size(48.dp),
            tint = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Text(
            stringResource(R.string.tab_needs_link),
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            textAlign = TextAlign.Center,
        )
        Button(onClick = onGo) { Text(stringResource(R.string.tab_earbuds)) }
    }
}

@Composable
private fun LogCard(log: List<String>) {
    if (log.isEmpty()) return
    Section(stringResource(R.string.section_log)) {
        log.takeLast(20).forEach {
            Text(it, fontSize = 12.sp, fontFamily = FontFamily.Monospace)
        }
    }
}

@Composable
private fun AboutLine() {
    val context = LocalContext.current
    val version = remember {
        context.packageManager.getPackageInfo(context.packageName, 0).versionName.orEmpty()
    }
    Text(
        stringResource(R.string.about_version, version),
        Modifier.fillMaxWidth(),
        fontSize = 12.sp,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        textAlign = TextAlign.Center,
    )
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
