package com.openpeats

import android.Manifest
import android.os.Build
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.compose.setContent
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.viewmodel.compose.viewModel
import com.openpeats.protocol.AncLevel
import com.openpeats.protocol.AncMode
import com.openpeats.protocol.EqBand
import com.openpeats.protocol.TransparencyLevel
import com.openpeats.ui.EqPanel

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

// Wire codes mapped to string resources. The codes are protocol constants and
// never change; only the label follows the system language.
private val ANC_LEVEL_LABELS = mapOf(
    AncLevel.ADAPTIVE to R.string.anc_level_adaptive,
    AncLevel.INDOOR to R.string.anc_level_indoor,
    AncLevel.OUTDOOR to R.string.anc_level_outdoor,
    AncLevel.TRAFFIC to R.string.anc_level_traffic,
)

private val TRANSPARENCY_LABELS = mapOf(
    TransparencyLevel.VOICE to R.string.transparency_voice,
    TransparencyLevel.STANDARD to R.string.transparency_standard,
)

// Voice prompt languages are endonyms: they read the same whatever the
// interface language is, so they stay out of the resources.
private val VOICE_PACKS = listOf(
    "English", "中文", "Deutsch", "日本語", "Français", "Español", "Italiano",
)

@Composable
fun App(vm: DeviceViewModel = viewModel()) {
    val state by vm.state.collectAsState()
    val log by vm.log.collectAsState()

    var granted by remember { mutableStateOf(Build.VERSION.SDK_INT < Build.VERSION_CODES.S) }
    val launcher = rememberLauncherForActivityResult(
        ActivityResultContracts.RequestMultiplePermissions()
    ) { res -> granted = res.values.all { it } }

    LaunchedEffect(Unit) {
        if (!granted) {
            launcher.launch(
                arrayOf(Manifest.permission.BLUETOOTH_CONNECT, Manifest.permission.BLUETOOTH_SCAN)
            )
        }
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
            Card(Modifier.fillMaxWidth()) {
                Text(stringResource(R.string.permission_required), Modifier.padding(16.dp))
            }
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
            DevicePicker(vm, state.connecting)
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

@Composable
private fun DevicePicker(vm: DeviceViewModel, connecting: Boolean) {
    val devices = remember { vm.pairedDevices() }

    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            Text(stringResource(R.string.paired_earbuds), fontWeight = FontWeight.Bold)

            if (devices.isEmpty()) {
                Text(stringResource(R.string.no_paired_devices), fontSize = 13.sp)
            }

            devices.forEach { d ->
                Button(
                    onClick = { vm.connect(d.address) },
                    enabled = !connecting,
                    modifier = Modifier.fillMaxWidth(),
                ) {
                    Text(
                        if (connecting) stringResource(R.string.connecting)
                        else stringResource(R.string.device_entry, d.name, d.address)
                    )
                }
            }

            Text(
                stringResource(R.string.multipoint_hint),
                fontSize = 12.sp,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

@Composable
private fun ConnectedView(vm: DeviceViewModel, s: DeviceState) {
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Row(
                Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Column {
                    Text(s.deviceName, fontWeight = FontWeight.Bold)
                    Text(
                        s.firmware.ifEmpty { stringResource(R.string.firmware_unknown) },
                        fontSize = 11.sp,
                        fontFamily = FontFamily.Monospace,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    TextButton(onClick = { vm.refresh() }, enabled = !s.busy) {
                        Text(stringResource(R.string.action_refresh))
                    }
                    TextButton(onClick = { vm.disconnect() }) {
                        Text(stringResource(R.string.action_close))
                    }
                }
            }

            Row(
                Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
            ) {
                Battery(stringResource(R.string.battery_left), s.batteryLeft)
                Battery(stringResource(R.string.battery_right), s.batteryRight)
                Battery(stringResource(R.string.battery_case), s.batteryCase)
            }
        }
    }

    Section(stringResource(R.string.section_anc)) {
        SegmentedRow(
            options = listOf(
                AncMode.OFF.toInt() to stringResource(R.string.anc_off),
                AncMode.ON.toInt() to stringResource(R.string.anc_on),
                AncMode.TRANSPARENCY.toInt() to stringResource(R.string.anc_transparency),
            ),
            selected = s.ancMode,
            enabled = !s.busy,
            onSelect = { vm.setAncMode(it) },
        )

        if (s.ancLevels.isNotEmpty()) {
            Spacer(Modifier.height(10.dp))
            Text(
                if (s.ancMode == AncMode.ON.toInt()) stringResource(R.string.anc_sub_mode)
                else stringResource(R.string.anc_sub_mode_inactive),
                fontSize = 12.sp,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Spacer(Modifier.height(6.dp))
            SegmentedRow(
                options = s.ancLevels.map { p ->
                    (p.code.toInt() and 0xFF) to labelFor(ANC_LEVEL_LABELS[p.code], p.code)
                },
                selected = s.ancLevel,
                enabled = !s.busy && s.ancMode == AncMode.ON.toInt(),
                onSelect = { vm.setAncLevel(it) },
            )
        }

        if (s.transparencyLevels.isNotEmpty()) {
            Spacer(Modifier.height(10.dp))
            Text(
                stringResource(R.string.transparency_level),
                fontSize = 12.sp,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Spacer(Modifier.height(6.dp))
            SegmentedRow(
                options = s.transparencyLevels.map { p ->
                    (p.code.toInt() and 0xFF) to labelFor(TRANSPARENCY_LABELS[p.code], p.code)
                },
                selected = s.transparencyLevel,
                enabled = !s.busy && s.ancMode == AncMode.TRANSPARENCY.toInt(),
                onSelect = { vm.setTransparencyLevel(it) },
            )
        }
    }

    Section(stringResource(R.string.section_codec)) {
        Text(
            stringResource(R.string.codec_hint),
            fontSize = 12.sp,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Spacer(Modifier.height(8.dp))
        SwitchRow(
            stringResource(R.string.toggle_ldac),
            s.ldac,
            !s.busy && "ldac" !in s.unsupported,
        ) { vm.setLdac(it) }
        SwitchRow(
            stringResource(R.string.toggle_multipoint),
            s.multipoint,
            !s.busy && "multipoint" !in s.unsupported,
        ) { vm.setMultipoint(it) }
        SwitchRow(
            stringResource(R.string.toggle_le_audio),
            s.leAudio,
            !s.busy && "leAudio" !in s.unsupported,
        ) { vm.setLeAudio(it) }
    }

    Section(stringResource(R.string.section_modes)) {
        SwitchRow(
            stringResource(R.string.toggle_game_mode),
            s.gameMode,
            !s.busy && "gameMode" !in s.unsupported,
        ) { vm.setGameMode(it) }
        SwitchRow(
            stringResource(R.string.toggle_touch_disabled),
            s.touchDisabled,
            !s.busy && "touchDisabled" !in s.unsupported,
        ) { vm.setTouchDisabled(it) }
    }

    if ("voicePack" !in s.unsupported) {
        Section(stringResource(R.string.section_voice_prompts)) {
            SegmentedRow(
                options = VOICE_PACKS.mapIndexed { i, n -> i to n },
                selected = s.voicePack,
                enabled = !s.busy,
                onSelect = { vm.setVoicePack(it) },
            )
            Spacer(Modifier.height(10.dp))
            Text(stringResource(R.string.prompt_volume, s.promptVolume), fontSize = 12.sp)
            var v by remember(s.promptVolume) { mutableFloatStateOf(s.promptVolume.toFloat()) }
            Slider(
                value = v,
                onValueChange = { v = it },
                onValueChangeFinished = { vm.setPromptVolume(v.toInt()) },
                valueRange = 0f..100f,
                enabled = !s.busy && "promptVolume" !in s.unsupported,
            )
        }
    }

    Section(stringResource(R.string.section_equaliser)) {
        EqPanel(busy = s.busy) { bands: List<EqBand>, master: Double ->
            vm.applyEq(bands, master)
        }
    }

    if (s.unsupported.isNotEmpty()) {
        Section(stringResource(R.string.section_unsupported)) {
            Text(
                s.unsupported.sorted().joinToString(" · "),
                fontSize = 12.sp,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

/**
 * Label of a wire code: the translated name when we know the code, the raw
 * hex value otherwise. Other SoundPEATS models expose codes this build has
 * never seen, and showing the byte beats showing nothing.
 */
@Composable
private fun labelFor(resId: Int?, code: Byte): String =
    if (resId != null) stringResource(resId) else "0x%02X".format(code)

@Composable
private fun Section(title: String, content: @Composable ColumnScope.() -> Unit) {
    // Explicit fillMaxWidth: a Card sizes itself on its content, so a card
    // holding only chips would come out narrower than the others.
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp)) {
            Text(title, fontWeight = FontWeight.Bold, fontSize = 15.sp)
            Spacer(Modifier.height(10.dp))
            content()
        }
    }
}

@Composable
private fun Battery(label: String, pct: Int) {
    Column(horizontalAlignment = Alignment.CenterHorizontally) {
        Text(label, fontSize = 11.sp, color = MaterialTheme.colorScheme.onSurfaceVariant)
        Text(
            stringResource(R.string.battery_percent, pct),
            fontSize = 19.sp,
            fontWeight = FontWeight.Bold,
        )
        LinearProgressIndicator(
            progress = { pct / 100f },
            modifier = Modifier.width(74.dp),
            color = when {
                pct <= 20 -> Color(0xFFE05E5E)
                pct <= 35 -> Color(0xFFE0A33E)
                else -> Color(0xFF3FBF7F)
            },
        )
    }
}

@Composable
private fun SwitchRow(
    label: String,
    checked: Boolean,
    enabled: Boolean,
    onChange: (Boolean) -> Unit,
) {
    Row(
        Modifier
            .fillMaxWidth()
            .padding(vertical = 3.dp),
        horizontalArrangement = Arrangement.SpaceBetween,
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(label, fontSize = 14.sp)
        Switch(checked = checked, onCheckedChange = onChange, enabled = enabled)
    }
}

@Composable
private fun SegmentedRow(
    options: List<Pair<Int, String>>,
    selected: Int,
    enabled: Boolean,
    onSelect: (Int) -> Unit,
) {
    FlowRowCompat {
        options.forEach { (value, label) ->
            FilterChip(
                selected = value == selected,
                onClick = { onSelect(value) },
                enabled = enabled,
                label = { Text(label, fontSize = 13.sp) },
                modifier = Modifier.padding(end = 6.dp, bottom = 6.dp),
            )
        }
    }
}

/** Wrapper: FlowRow is still marked experimental in Compose. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun FlowRowCompat(content: @Composable () -> Unit) {
    FlowRow { content() }
}
