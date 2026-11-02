package com.openpeats.ui

import android.text.format.DateFormat
import androidx.compose.foundation.layout.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.openpeats.DeviceViewModel
import com.openpeats.R
import com.openpeats.protocol.AncMode
import com.openpeats.session.DeviceState
import com.openpeats.session.KnownEarbuds

// Names of the features the refresh can find absent, keyed like
// DeviceState.unsupported. A key missing here still shows, raw, rather than
// vanishing from the list.
private val FEATURE_LABELS = mapOf(
    "batteryLeft" to R.string.feature_battery_left,
    "batteryRight" to R.string.feature_battery_right,
    "batteryCase" to R.string.feature_battery_case,
    "firmware" to R.string.feature_firmware,
    "ancMode" to R.string.feature_anc_mode,
    "ancLevel" to R.string.feature_anc_level,
    "transparencyLevel" to R.string.feature_transparency_level,
    "gameMode" to R.string.feature_game_mode,
    "multipoint" to R.string.feature_multipoint,
    "ldac" to R.string.feature_ldac,
    "leAudio" to R.string.feature_le_audio,
    "touchDisabled" to R.string.feature_touch_disabled,
    "promptVolume" to R.string.feature_prompt_volume,
    "voicePack" to R.string.feature_voice_pack,
    "keyMappings" to R.string.feature_key_mappings,
)

// Voice prompt languages are endonyms: they read the same whatever the
// interface language is, so they stay out of the resources.
private val VOICE_PACKS = listOf(
    "English", "中文", "Deutsch", "日本語", "Français", "Español", "Italiano",
)

private val MODES = listOf(
    Triple(AncMode.OFF.toInt(), R.string.anc_off, R.drawable.ic_mode_off),
    Triple(AncMode.ON.toInt(), R.string.anc_on, R.drawable.ic_mode_anc),
    Triple(AncMode.TRANSPARENCY.toInt(), R.string.anc_transparency, R.drawable.ic_mode_transparency),
)

/** What you reach for most: the batteries and the noise modes. */
@Composable
fun EarbudsControls(vm: DeviceViewModel, s: DeviceState) {
    Card(Modifier.fillMaxWidth()) {
        Row(
            Modifier
                .fillMaxWidth()
                .padding(16.dp),
            horizontalArrangement = Arrangement.SpaceBetween,
        ) {
            if ("batteryLeft" !in s.unsupported) Battery(stringResource(R.string.battery_left), s.batteryLeft)
            if ("batteryRight" !in s.unsupported) Battery(stringResource(R.string.battery_right), s.batteryRight)
            if ("batteryCase" !in s.unsupported) Battery(stringResource(R.string.battery_case), s.batteryCase)
        }
    }

    if ("ancMode" !in s.unsupported) {
        Section(stringResource(R.string.section_anc)) {
            NoiseModes(s.ancMode, enabled = true) { vm.setAncMode(it) }
            SubModes(vm, s)
        }
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
}

/** The three noise modes, side by side, each with its icon. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun NoiseModes(selected: Int, enabled: Boolean, onSelect: (Int) -> Unit) {
    SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth()) {
        MODES.forEachIndexed { i, (mode, label, icon) ->
            SegmentedButton(
                selected = mode == selected,
                onClick = { onSelect(mode) },
                enabled = enabled,
                shape = SegmentedButtonDefaults.itemShape(i, MODES.size),
                icon = { },
                label = {
                    Column(horizontalAlignment = Alignment.CenterHorizontally) {
                        Icon(painterResource(icon), null, Modifier.size(20.dp))
                        Text(stringResource(label), fontSize = 12.sp, maxLines = 1)
                    }
                },
            )
        }
    }
}

/** The sub-modes of the mode that is on. Only one set ever applies, so only one shows. */
@Composable
private fun SubModes(vm: DeviceViewModel, s: DeviceState) {
    when {
        s.ancMode == AncMode.ON.toInt() && s.ancLevels.isNotEmpty() -> {
            Spacer(Modifier.height(12.dp))
            Hint(stringResource(R.string.anc_sub_mode))
            Spacer(Modifier.height(6.dp))
            SegmentedRow(
                options = s.ancLevels.map { p ->
                    (p.code.toInt() and 0xFF) to labelFor(ANC_LEVEL_LABELS[p.code], p.code)
                },
                selected = s.ancLevel,
                enabled = !s.busy,
                onSelect = { vm.setAncLevel(it) },
            )
        }
        s.ancMode == AncMode.TRANSPARENCY.toInt() && s.transparencyLevels.isNotEmpty() -> {
            Spacer(Modifier.height(12.dp))
            Hint(stringResource(R.string.transparency_level))
            Spacer(Modifier.height(6.dp))
            SegmentedRow(
                options = s.transparencyLevels.map { p ->
                    (p.code.toInt() and 0xFF) to labelFor(TRANSPARENCY_LABELS[p.code], p.code)
                },
                selected = s.transparencyLevel,
                enabled = !s.busy,
                onSelect = { vm.setTransparencyLevel(it) },
            )
        }
    }
}

/** With no link: the batteries as last read, labelled with when. */
@Composable
fun LastRead(known: KnownEarbuds) {
    val time = DateFormat.getTimeFormat(LocalContext.current).format(known.readAtMs)
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            Row(
                Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(known.name, fontWeight = FontWeight.Bold)
                Hint(stringResource(R.string.status_last_read, time))
            }
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
                if ("batteryLeft" !in known.unsupported) {
                    Battery(stringResource(R.string.battery_left), known.batteryLeft)
                }
                if ("batteryRight" !in known.unsupported) {
                    Battery(stringResource(R.string.battery_right), known.batteryRight)
                }
                if ("batteryCase" !in known.unsupported) {
                    Battery(stringResource(R.string.battery_case), known.batteryCase)
                }
            }
        }
    }
}

/** The settings you set once: the codec, the voice prompts, what this model is. */
@Composable
fun EarbudsSettings(vm: DeviceViewModel, s: DeviceState, address: String?) {
    Section(stringResource(R.string.section_codec)) {
        Hint(stringResource(R.string.codec_hint))
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

    Section(stringResource(R.string.section_device)) {
        Text(s.deviceName, fontWeight = FontWeight.Bold, fontSize = 14.sp)
        Spacer(Modifier.height(4.dp))
        address?.let {
            Text(
                it,
                fontSize = 12.sp,
                fontFamily = FontFamily.Monospace,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        Text(
            s.firmware.ifEmpty { stringResource(R.string.firmware_unknown) },
            fontSize = 12.sp,
            fontFamily = FontFamily.Monospace,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        if (s.unsupported.isNotEmpty()) {
            Spacer(Modifier.height(12.dp))
            Hint(stringResource(R.string.section_unsupported))
            Spacer(Modifier.height(4.dp))
            Hint(
                s.unsupported
                    .map { key -> FEATURE_LABELS[key]?.let { stringResource(it) } ?: key }
                    .sorted()
                    .joinToString(" · ")
            )
        }
    }
}

@Composable
private fun Hint(text: String) {
    Text(text, fontSize = 12.sp, color = MaterialTheme.colorScheme.onSurfaceVariant)
}
