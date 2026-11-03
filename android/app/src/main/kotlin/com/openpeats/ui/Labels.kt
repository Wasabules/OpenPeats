package com.openpeats.ui

import androidx.annotation.StringRes
import com.openpeats.R
import com.openpeats.protocol.AncLevel
import com.openpeats.protocol.TransparencyLevel

// Wire codes mapped to string resources. The codes are protocol constants and
// never change; only the label follows the system language.
internal val ANC_LEVEL_LABELS = mapOf(
    AncLevel.ADAPTIVE to R.string.anc_level_adaptive,
    AncLevel.INDOOR to R.string.anc_level_indoor,
    AncLevel.OUTDOOR to R.string.anc_level_outdoor,
    AncLevel.TRAFFIC to R.string.anc_level_traffic,
)

internal val TRANSPARENCY_LABELS = mapOf(
    TransparencyLevel.VOICE to R.string.transparency_voice,
    TransparencyLevel.STANDARD to R.string.transparency_standard,
)

/** The label of a sub-mode code, for widgets. A code this build has never seen gets the generic name. */
@StringRes
internal fun ancLevelLabel(code: Int): Int = ANC_LEVEL_LABELS[code.toByte()] ?: R.string.anc_sub_mode

@StringRes
internal fun transparencyLabel(code: Int): Int =
    TRANSPARENCY_LABELS[code.toByte()] ?: R.string.transparency_level
