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
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.viewmodel.compose.viewModel
import com.openpeats.ui.ConnectedView
import com.openpeats.ui.DevicePicker

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
            DevicePicker(vm, state)
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
