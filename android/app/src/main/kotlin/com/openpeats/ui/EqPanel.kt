package com.openpeats.ui

import androidx.compose.foundation.layout.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.foundation.Canvas
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.openpeats.protocol.EqBand
import kotlin.math.PI
import kotlin.math.cos
import kotlin.math.hypot
import kotlin.math.log10
import kotlin.math.pow
import kotlin.math.sin

private const val FS = 48000.0
private const val F_MIN = 20.0
private const val F_MAX = 20000.0
private const val DB_RANGE = 12.0

/** Jeux de fréquences centrales normalisés — « finesse » du réglage. */
private val GRANULARITY = mapOf(
    5 to listOf(60, 250, 1000, 4000, 12000),
    8 to listOf(40, 100, 250, 630, 1600, 4000, 10000, 16000),
    10 to listOf(31, 62, 125, 250, 500, 1000, 2000, 4000, 8000, 16000),
)

/** Réponse d'un filtre peaking RBJ, en dB, à la fréquence f. */
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

@Composable
fun EqPanel(busy: Boolean, onApply: (List<EqBand>, Double) -> Unit) {
    var granularity by remember { mutableIntStateOf(8) }
    var masterGain by remember { mutableDoubleStateOf(0.0) }
    var bands by remember {
        mutableStateOf(GRANULARITY[8]!!.map { EqBand(it, 0.0, 1.4) })
    }

    fun setGranularity(n: Int) {
        val old = bands
        granularity = n
        // On conserve l'allure : le gain de chaque nouvelle bande est
        // interpolé depuis la courbe actuelle.
        bands = GRANULARITY[n]!!.map { f ->
            val g = old.sumOf { peakingDb(f.toDouble(), it) }
            EqBand(f, g.coerceIn(-12.0, 12.0), 1.4)
        }
    }

    Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
        Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            Text("Finesse", fontSize = 12.sp, modifier = Modifier.align(Alignment.CenterVertically))
            GRANULARITY.keys.sorted().forEach { n ->
                FilterChip(
                    selected = granularity == n,
                    onClick = { setGranularity(n) },
                    enabled = !busy,
                    label = { Text("$n", fontSize = 12.sp) },
                )
            }
        }

        EqCurve(bands, masterGain)

        // Faders : un par bande, avec le Q juste au-dessus.
        Row(
            Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(2.dp),
        ) {
            bands.forEachIndexed { i, b ->
                Column(
                    Modifier.weight(1f),
                    horizontalAlignment = Alignment.CenterHorizontally,
                ) {
                    Text("Q%.1f".format(b.q), fontSize = 9.sp)
                    Slider(
                        value = b.q.toFloat(),
                        onValueChange = { v ->
                            bands = bands.toMutableList()
                                .also { it[i] = b.copy(q = v.toDouble()) }
                        },
                        valueRange = 0.3f..6f,
                        enabled = !busy,
                        modifier = Modifier.height(28.dp),
                    )
                    Text(
                        "%+.1f".format(b.gain),
                        fontSize = 10.sp,
                        color = if (b.gain == 0.0)
                            MaterialTheme.colorScheme.onSurfaceVariant else Color.Unspecified,
                    )
                    Slider(
                        value = b.gain.toFloat(),
                        onValueChange = { v ->
                            bands = bands.toMutableList()
                                .also { it[i] = b.copy(gain = (v * 2).toInt() / 2.0) }
                        },
                        valueRange = -12f..12f,
                        enabled = !busy,
                        modifier = Modifier.height(30.dp),
                    )
                    Text(fmtFreq(b.freq), fontSize = 9.sp)
                }
            }
        }

        Text("Gain maître %+.1f dB".format(masterGain), fontSize = 12.sp)
        Slider(
            value = masterGain.toFloat(),
            onValueChange = { masterGain = (it * 2).toInt() / 2.0 },
            valueRange = -12f..12f,
            enabled = !busy,
        )

        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            OutlinedButton(
                onClick = { bands = bands.map { it.copy(gain = 0.0) }; masterGain = 0.0 },
                enabled = !busy,
            ) { Text("Remettre à zéro") }

            Button(
                onClick = { onApply(bands, masterGain) },
                enabled = !busy,
                modifier = Modifier.weight(1f),
            ) { Text(if (busy) "Application…" else "Appliquer") }
        }

        Text(
            "Courbe calculée localement (filtres peaking RBJ à 48 kHz) : " +
                "prévisualisation, pas une mesure.",
            fontSize = 11.sp,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}

@Composable
private fun EqCurve(bands: List<EqBand>, masterGain: Double) {
    val line = MaterialTheme.colorScheme.primary
    val grid = MaterialTheme.colorScheme.outlineVariant

    Canvas(
        Modifier
            .fillMaxWidth()
            .height(120.dp)
    ) {
        val w = size.width
        val h = size.height
        val mid = h / 2

        // Ligne 0 dB.
        drawLine(grid, androidx.compose.ui.geometry.Offset(0f, mid),
            androidx.compose.ui.geometry.Offset(w, mid), strokeWidth = 1f)

        val path = Path()
        val steps = 120
        for (i in 0..steps) {
            val f = F_MIN * (F_MAX / F_MIN).pow(i.toDouble() / steps)
            val db = (masterGain + bands.sumOf { peakingDb(f, it) })
                .coerceIn(-DB_RANGE, DB_RANGE)
            val x = w * i / steps
            val y = (mid - (db / DB_RANGE) * (mid * 0.9)).toFloat()
            if (i == 0) path.moveTo(x, y) else path.lineTo(x, y)
        }
        drawPath(path, line, style = androidx.compose.ui.graphics.drawscope.Stroke(width = 3f))
    }
}

private fun fmtFreq(f: Int): String =
    if (f >= 1000) "${f / 1000}k" else "$f"
