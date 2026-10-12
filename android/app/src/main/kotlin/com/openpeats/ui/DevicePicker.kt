package com.openpeats.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.openpeats.DeviceState
import com.openpeats.DeviceViewModel
import com.openpeats.R

/** The paired earbuds to connect to, shown while no link is open. */
@Composable
fun DevicePicker(vm: DeviceViewModel, s: DeviceState) {
    val devices = remember { vm.pairedDevices() }
    val connecting = s.connecting

    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            Text(stringResource(R.string.paired_earbuds), fontWeight = FontWeight.Bold)

            // Picking a device by hand stays possible: it supersedes the
            // automatic reconnection.
            if (s.reconnecting) {
                Row(
                    Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.SpaceBetween,
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text(stringResource(R.string.reconnecting), fontSize = 13.sp, modifier = Modifier.weight(1f))
                    TextButton(onClick = { vm.disconnect() }) {
                        Text(stringResource(R.string.action_stop))
                    }
                }
            }

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
