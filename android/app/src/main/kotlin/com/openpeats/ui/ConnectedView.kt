package com.openpeats.ui

import androidx.compose.foundation.layout.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.openpeats.DeviceState
import com.openpeats.DeviceViewModel
import com.openpeats.R
import com.openpeats.protocol.AncLevel
import com.openpeats.protocol.AncMode
import com.openpeats.protocol.EqBand
import com.openpeats.protocol.TransparencyLevel

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

/** Everything shown once the control link is open. */
@Composable
fun ConnectedView(vm: DeviceViewModel, s: DeviceState) {
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
                s.unsupported
                    .map { key -> FEATURE_LABELS[key]?.let { stringResource(it) } ?: key }
                    .sorted()
                    .joinToString(" · "),
                fontSize = 12.sp,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}
