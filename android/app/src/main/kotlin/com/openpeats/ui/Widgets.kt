package com.openpeats.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.material3.Card
import androidx.compose.material3.FilterChip
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.openpeats.R

// Small building blocks shared by the screens.

/**
 * Label of a wire code: the translated name when we know the code, the raw
 * hex value otherwise. Other SoundPEATS models expose codes this build has
 * never seen, and showing the byte beats showing nothing.
 */
@Composable
internal fun labelFor(resId: Int?, code: Byte): String =
    if (resId != null) stringResource(resId) else "0x%02X".format(code)

@Composable
internal fun Section(title: String, content: @Composable ColumnScope.() -> Unit) {
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
internal fun Battery(label: String, pct: Int) {
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
internal fun SwitchRow(
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
internal fun SegmentedRow(
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
