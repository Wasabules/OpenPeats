package com.openpeats.ui

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.gestures.detectDragGestures
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.openpeats.R
import com.openpeats.protocol.EqBand
import kotlin.math.PI
import kotlin.math.cos
import kotlin.math.hypot
import kotlin.math.log10
import kotlin.math.pow
import kotlin.math.roundToInt
import kotlin.math.sin

private const val FS = 48000.0
private const val F_MIN = 20.0
private const val F_MAX = 20000.0
private const val DB_RANGE = 12.0
private const val GAIN_MIN = -12.0
private const val GAIN_MAX = 12.0

/** Normalised sets of centre frequencies — the "granularity" of the control. */
private val GRANULARITY = mapOf(
    5 to listOf(60, 250, 1000, 4000, 12000),
    8 to listOf(40, 100, 250, 630, 1600, 4000, 10000, 16000),
    10 to listOf(31, 62, 125, 250, 500, 1000, 2000, 4000, 8000, 16000),
)

/**
 * A preset: the resource holding its name, and the transform it applies.
 * Same set as the desktop version.
 */
private data class Preset(val labelRes: Int, val apply: (List<EqBand>) -> List<EqBand>)

private val PRESETS = listOf(
    Preset(R.string.eq_preset_flat) { bs -> bs.map { it.copy(gain = 0.0) } },
    Preset(R.string.eq_preset_bass) { bs ->
        bs.map {
            it.copy(gain = if (it.freq <= 250) 6.0 else if (it.freq <= 500) 2.0 else 0.0)
        }
    },
    Preset(R.string.eq_preset_vocal) { bs ->
        bs.map {
            it.copy(gain = if (it.freq < 300) -3.0 else if (it.freq <= 4000) 4.0 else 0.0)
        }
    },
    Preset(R.string.eq_preset_treble) { bs ->
        bs.map {
            it.copy(gain = if (it.freq >= 6000) 5.0 else if (it.freq >= 3000) 2.0 else 0.0)
        }
    },
    Preset(R.string.eq_preset_loudness) { bs ->
        bs.map {
            it.copy(
                gain = when {
                    it.freq <= 120 -> 6.0
                    it.freq >= 8000 -> 5.0
                    it.freq in 500..2000 -> -2.0
                    else -> 0.0
                }
            )
        }
    },
)

/** Response of an RBJ peaking filter, in dB, at frequency f. */
private fun peakingDb(f: Double, b: EqBand): Double {
    if (b.gain == 0.0) return 0.0
    val a = 10.0.pow(b.gain / 40.0)
    val w0 = 2 * PI * b.freq / FS
    val alpha = sin(w0) / (2 * maxOf(b.q, 0.05))

    val b0 = 1 + alpha * a; val b1 = -2 * cos(w0); val b2 = 1 - alpha * a
    val a0 = 1 + alpha / a; val a1 = -2 * cos(w0); val a2 = 1 - alpha / a

    val w = 2 * PI * f / FS
    val cw = cos(w); val sw = sin(w)
    val c2w = cos(2 * w); val s2w = sin(2 * w)

    val nRe = b0 + b1 * cw + b2 * c2w; val nIm = -(b1 * sw + b2 * s2w)
    val dRe = a0 + a1 * cw + a2 * c2w; val dIm = -(a1 * sw + a2 * s2w)

    val den = hypot(dRe, dIm)
    if (den == 0.0) return 0.0
    return 20 * log10(hypot(nRe, nIm) / den)
}

private fun fmtFreq(f: Int): String =
    if (f >= 1000) "${f / 1000}k" else "$f"

@Composable
fun EqPanel(busy: Boolean, onApply: (List<EqBand>, Double) -> Unit) {
    var granularity by remember { mutableIntStateOf(5) }
    var masterGain by remember { mutableDoubleStateOf(0.0) }
    var selected by remember { mutableIntStateOf(0) }
    var bands by remember {
        mutableStateOf(GRANULARITY[5]!!.map { EqBand(it, 0.0, 1.4) })
    }

    fun setGranularity(n: Int) {
        val old = bands
        granularity = n
        // Keep the shape: the gain of each new band is interpolated from the
        // current curve.
        bands = GRANULARITY[n]!!.map { f ->
            val g = old.sumOf { peakingDb(f.toDouble(), it) }
            EqBand(f, g.coerceIn(GAIN_MIN, GAIN_MAX), 1.4)
        }
        selected = selected.coerceAtMost(bands.lastIndex)
    }

    Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {

        // --- Presets ---
        Text(stringResource(R.string.eq_presets), fontSize = 12.sp,
            color = MaterialTheme.colorScheme.onSurfaceVariant)
        LazyRowCompat {
            PRESETS.forEach { preset ->
                AssistChip(
                    onClick = { bands = preset.apply(bands); masterGain = 0.0 },
                    enabled = !busy,
                    label = { Text(stringResource(preset.labelRes), fontSize = 12.sp) },
                    modifier = Modifier.padding(end = 6.dp),
                )
            }
        }

        // --- Granularity ---
        Row(
            horizontalArrangement = Arrangement.spacedBy(6.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(stringResource(R.string.eq_granularity), fontSize = 12.sp,
                color = MaterialTheme.colorScheme.onSurfaceVariant)
            GRANULARITY.keys.sorted().forEach { n ->
                FilterChip(
                    selected = granularity == n,
                    onClick = { setGranularity(n) },
                    enabled = !busy,
                    label = { Text(stringResource(R.string.eq_bands, n), fontSize = 12.sp) },
                )
            }
        }

        EqCurve(bands, masterGain)

        // --- Bank of vertical faders ---
        FaderBank(
            bands = bands,
            selected = selected,
            enabled = !busy,
            onSelect = { selected = it },
            onGain = { i, g ->
                bands = bands.toMutableList().also { it[i] = it[i].copy(gain = g) }
            },
        )

        // --- Detail of the selected band ---
        val b = bands.getOrNull(selected)
        if (b != null) {
            Card(colors = CardDefaults.cardColors(
                containerColor = MaterialTheme.colorScheme.surfaceVariant)) {
                Column(Modifier.padding(12.dp)) {
                    Row(
                        Modifier.fillMaxWidth(),
                        horizontalArrangement = Arrangement.SpaceBetween,
                    ) {
                        Text(
                            stringResource(
                                R.string.eq_band_detail, selected + 1, fmtFreq(b.freq),
                            ),
                            fontWeight = FontWeight.Bold, fontSize = 14.sp,
                        )
                        Text(stringResource(R.string.eq_gain_db, b.gain), fontSize = 14.sp)
                    }

                    Spacer(Modifier.height(8.dp))
                    Text(stringResource(R.string.eq_bandwidth, b.q), fontSize = 12.sp)
                    Slider(
                        value = b.q.toFloat(),
                        onValueChange = { v ->
                            bands = bands.toMutableList()
                                .also { it[selected] = b.copy(q = v.toDouble()) }
                        },
                        valueRange = 0.3f..6f,
                        enabled = !busy,
                    )
                    Text(
                        stringResource(R.string.eq_bandwidth_hint),
                        fontSize = 11.sp,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
        }

        Text(stringResource(R.string.eq_master_gain, masterGain), fontSize = 12.sp)
        Slider(
            value = masterGain.toFloat(),
            onValueChange = { masterGain = (it * 2).roundToInt() / 2.0 },
            valueRange = GAIN_MIN.toFloat()..GAIN_MAX.toFloat(),
            enabled = !busy,
        )

        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            OutlinedButton(
                onClick = { bands = bands.map { it.copy(gain = 0.0) }; masterGain = 0.0 },
                enabled = !busy,
            ) { Text(stringResource(R.string.action_zero)) }

            Button(
                onClick = { onApply(bands, masterGain) },
                enabled = !busy,
                modifier = Modifier.weight(1f),
            ) {
                Text(
                    if (busy) stringResource(R.string.action_applying)
                    else stringResource(R.string.action_apply)
                )
            }
        }
    }
}

/**
 * Bank of vertical faders drawn in one go.
 *
 * A single Canvas rather than N Sliders: with 8 or 10 bands, stacked
 * components become too narrow to hit. Here each band's touch area covers its
 * whole column.
 */
@Composable
private fun FaderBank(
    bands: List<EqBand>,
    selected: Int,
    enabled: Boolean,
    onSelect: (Int) -> Unit,
    onGain: (Int, Double) -> Unit,
) {
    val accent = MaterialTheme.colorScheme.primary
    val track = MaterialTheme.colorScheme.surfaceVariant
    val dim = MaterialTheme.colorScheme.onSurfaceVariant
    val density = LocalDensity.current

    // Turns an ordinate into a gain, rounded to half a decibel and clamped.
    fun gainAt(y: Float, h: Float): Double {
        val usable = h - 40f
        val mid = 20f + usable / 2f
        val g = ((mid - y) / (usable / 2f)) * GAIN_MAX
        return ((g * 2).roundToInt() / 2.0).coerceIn(GAIN_MIN, GAIN_MAX)
    }

    Column {
        Canvas(
            Modifier
                .fillMaxWidth()
                .height(190.dp)
                .pointerInput(bands.size, enabled) {
                    if (!enabled) return@pointerInput
                    detectTapGestures { off ->
                        val i = (off.x / (size.width / bands.size))
                            .toInt().coerceIn(0, bands.lastIndex)
                        onSelect(i)
                        onGain(i, gainAt(off.y, size.height.toFloat())
                            .coerceIn(GAIN_MIN, GAIN_MAX))
                    }
                }
                .pointerInput(bands.size, enabled) {
                    if (!enabled) return@pointerInput
                    var idx = -1
                    detectDragGestures(
                        onDragStart = { off ->
                            idx = (off.x / (size.width / bands.size))
                                .toInt().coerceIn(0, bands.lastIndex)
                            onSelect(idx)
                        },
                        onDragEnd = { idx = -1 },
                    ) { change, _ ->
                        if (idx >= 0) {
                            onGain(
                                idx,
                                gainAt(change.position.y, size.height.toFloat())
                                    .coerceIn(GAIN_MIN, GAIN_MAX),
                            )
                        }
                    }
                }
        ) {
            val colW = size.width / bands.size
            val usable = size.height - 40f
            val top = 20f
            val mid = top + usable / 2f
            val trackW = minOf(colW * 0.30f, 16f)

            // 0 dB reference line.
            drawLine(dim.copy(alpha = 0.35f), Offset(0f, mid), Offset(size.width, mid),
                strokeWidth = 1f)

            bands.forEachIndexed { i, b ->
                val cx = colW * i + colW / 2f
                val y = (mid - (b.gain / GAIN_MAX) * (usable / 2f)).toFloat()

                // Track.
                drawRoundRect(
                    color = track,
                    topLeft = Offset(cx - trackW / 2f, top),
                    size = androidx.compose.ui.geometry.Size(trackW, usable),
                    cornerRadius = androidx.compose.ui.geometry.CornerRadius(trackW / 2f),
                )

                // Fill from the zero line.
                val fillTop = minOf(y, mid)
                val fillH = kotlin.math.abs(y - mid)
                if (fillH > 1f) {
                    drawRoundRect(
                        color = accent.copy(alpha = if (i == selected) 1f else 0.65f),
                        topLeft = Offset(cx - trackW / 2f, fillTop),
                        size = androidx.compose.ui.geometry.Size(trackW, fillH),
                        cornerRadius = androidx.compose.ui.geometry.CornerRadius(trackW / 2f),
                    )
                }

                // Handle.
                drawCircle(
                    color = if (i == selected) accent else accent.copy(alpha = 0.8f),
                    radius = if (i == selected) 11f else 9f,
                    center = Offset(cx, y),
                )
                drawCircle(Color.Black.copy(alpha = 0.55f), radius = 3.5f, Offset(cx, y))
            }
        }

        // Frequency labels under each fader.
        Row(Modifier.fillMaxWidth()) {
            bands.forEachIndexed { i, b ->
                Text(
                    fmtFreq(b.freq),
                    fontSize = 10.sp,
                    fontWeight = if (i == selected) FontWeight.Bold else FontWeight.Normal,
                    color = if (i == selected) MaterialTheme.colorScheme.primary else dim,
                    modifier = Modifier.weight(1f),
                    textAlign = androidx.compose.ui.text.style.TextAlign.Center,
                )
            }
        }
    }
}

@Composable
private fun EqCurve(bands: List<EqBand>, masterGain: Double) {
    val line = MaterialTheme.colorScheme.primary
    val grid = MaterialTheme.colorScheme.outlineVariant

    Canvas(
        Modifier
            .fillMaxWidth()
            .height(110.dp)
    ) {
        val w = size.width
        val h = size.height
        val mid = h / 2

        drawLine(grid, Offset(0f, mid), Offset(w, mid), strokeWidth = 1f)

        val path = Path()
        val steps = 140
        for (i in 0..steps) {
            val f = F_MIN * (F_MAX / F_MIN).pow(i.toDouble() / steps)
            val db = (masterGain + bands.sumOf { peakingDb(f, it) })
                .coerceIn(-DB_RANGE, DB_RANGE)
            val x = w * i / steps
            val y = (mid - (db / DB_RANGE) * (mid * 0.88)).toFloat()
            if (i == 0) path.moveTo(x, y) else path.lineTo(x, y)
        }
        drawPath(path, line, style = Stroke(width = 4f))
    }
}

/** Horizontally scrolling row, without depending on FlowRow. */
@Composable
private fun LazyRowCompat(content: @Composable RowScope.() -> Unit) {
    Row(
        Modifier
            .fillMaxWidth()
            .horizontalScroll(rememberScrollState()),
        content = content,
    )
}
