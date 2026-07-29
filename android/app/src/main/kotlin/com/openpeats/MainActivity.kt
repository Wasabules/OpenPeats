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

private val ANC_LEVEL_LABELS = mapOf(
    AncLevel.ADAPTIVE to "Adaptative",
    AncLevel.INDOOR to "Intérieur",
    AncLevel.OUTDOOR to "Extérieur",
    AncLevel.TRAFFIC to "Trafic",
)

private val TRANSPARENCY_LABELS = mapOf(
    TransparencyLevel.VOICE to "Voix renforcée",
    TransparencyLevel.STANDARD to "Standard",
)

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

    // targetSdk 35 impose l'affichage bord à bord : sans cet encart, le contenu
    // passe sous la barre d'état et sous les boutons de navigation.
    Column(
        Modifier
            .fillMaxSize()
            .windowInsetsPadding(WindowInsets.safeDrawing)
            .verticalScroll(rememberScrollState())
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(14.dp),
    ) {
        Text("OpenPeats", fontSize = 26.sp, fontWeight = FontWeight.Bold)

        if (!granted) {
            Card(Modifier.fillMaxWidth()) {
                Text("Autorisation Bluetooth requise.", Modifier.padding(16.dp))
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
                    Text("Journal", fontWeight = FontWeight.Bold, fontSize = 15.sp)
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
            Text("Écouteurs appairés", fontWeight = FontWeight.Bold)

            if (devices.isEmpty()) {
                Text(
                    "Aucun appareil audio appairé. Appairez vos écouteurs dans les " +
                        "réglages Bluetooth, puis revenez ici.",
                    fontSize = 13.sp,
                )
            }

            devices.forEach { d ->
                Button(
                    onClick = { vm.connect(d.address) },
                    enabled = !connecting,
                    modifier = Modifier.fillMaxWidth(),
                ) {
                    Text(if (connecting) "Connexion…" else "${d.name}  ·  ${d.address}")
                }
            }

            Text(
                "Si le multipoint est actif et qu'un autre appareil tient le canal " +
                    "de contrôle, la connexion échouera : coupez-y le Bluetooth.",
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
                        s.firmware.ifEmpty { "firmware ?" },
                        fontSize = 11.sp,
                        fontFamily = FontFamily.Monospace,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    TextButton(onClick = { vm.refresh() }, enabled = !s.busy) { Text("Relire") }
                    TextButton(onClick = { vm.disconnect() }) { Text("Fermer") }
                }
            }

            Row(
                Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
            ) {
                Battery("Gauche", s.batteryLeft)
                Battery("Droite", s.batteryRight)
                Battery("Étui", s.batteryCase)
            }
        }
    }

    Section("Réduction de bruit") {
        SegmentedRow(
            options = listOf(
                AncMode.OFF.toInt() to "Arrêt",
                AncMode.ON.toInt() to "ANC",
                AncMode.TRANSPARENCY.toInt() to "Transparence",
            ),
            selected = s.ancMode,
            enabled = !s.busy,
            onSelect = { vm.setAncMode(it) },
        )

        if (s.ancLevels.isNotEmpty()) {
            Spacer(Modifier.height(10.dp))
            Text(
                "Sous-mode" + if (s.ancMode != AncMode.ON.toInt())
                    " — actif en réduction de bruit seulement" else "",
                fontSize = 12.sp,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Spacer(Modifier.height(6.dp))
            SegmentedRow(
                options = s.ancLevels.map { p ->
                    (p.code.toInt() and 0xFF) to
                        (ANC_LEVEL_LABELS[p.code] ?: "0x%02X".format(p.code))
                },
                selected = s.ancLevel,
                enabled = !s.busy && s.ancMode == AncMode.ON.toInt(),
                onSelect = { vm.setAncLevel(it) },
            )
        }

        if (s.transparencyLevels.isNotEmpty()) {
            Spacer(Modifier.height(10.dp))
            Text(
                "Niveau de transparence",
                fontSize = 12.sp,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Spacer(Modifier.height(6.dp))
            SegmentedRow(
                options = s.transparencyLevels.map { p ->
                    (p.code.toInt() and 0xFF) to
                        (TRANSPARENCY_LABELS[p.code] ?: "0x%02X".format(p.code))
                },
                selected = s.transparencyLevel,
                enabled = !s.busy && s.ancMode == AncMode.TRANSPARENCY.toInt(),
                onSelect = { vm.setTransparencyLevel(it) },
            )
        }
    }

    Section("Codec et connectivité") {
        Text(
            "LDAC et multipoint sont exclusifs : le firmware coupe LDAC dès que " +
                "le multipoint s'active, faute de bande passante pour deux liens.",
            fontSize = 12.sp,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Spacer(Modifier.height(8.dp))
        SwitchRow("LDAC", s.ldac, !s.busy && "ldac" !in s.unsupported) { vm.setLdac(it) }
        SwitchRow("Multipoint", s.multipoint, !s.busy && "multipoint" !in s.unsupported) {
            vm.setMultipoint(it)
        }
        SwitchRow("LE Audio", s.leAudio, !s.busy && "leAudio" !in s.unsupported) {
            vm.setLeAudio(it)
        }
    }

    Section("Modes") {
        SwitchRow("Mode jeu", s.gameMode, !s.busy && "gameMode" !in s.unsupported) {
            vm.setGameMode(it)
        }
        SwitchRow(
            "Désactiver le tactile",
            s.touchDisabled,
            !s.busy && "touchDisabled" !in s.unsupported,
        ) { vm.setTouchDisabled(it) }
    }

    if ("voicePack" !in s.unsupported) {
        Section("Invites vocales") {
            SegmentedRow(
                options = VOICE_PACKS.mapIndexed { i, n -> i to n },
                selected = s.voicePack,
                enabled = !s.busy,
                onSelect = { vm.setVoicePack(it) },
            )
            Spacer(Modifier.height(10.dp))
            Text("Volume — ${s.promptVolume}", fontSize = 12.sp)
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

    Section("Égaliseur") {
        EqPanel(busy = s.busy) { bands: List<EqBand>, master: Double ->
            vm.applyEq(bands, master)
        }
    }

    if (s.unsupported.isNotEmpty()) {
        Section("Absent de ce modèle") {
            Text(
                s.unsupported.sorted().joinToString(" · "),
                fontSize = 12.sp,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

@Composable
private fun Section(title: String, content: @Composable ColumnScope.() -> Unit) {
    // fillMaxWidth explicite : un Card se dimensionne sur son contenu, donc
    // une carte ne contenant que des chips serait plus étroite que les autres.
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
        Text("$pct%", fontSize = 19.sp, fontWeight = FontWeight.Bold)
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

/** Enveloppe : FlowRow est encore marqué expérimental dans Compose. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun FlowRowCompat(content: @Composable () -> Unit) {
    FlowRow { content() }
}
