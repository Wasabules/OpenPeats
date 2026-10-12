package com.openpeats

import android.annotation.SuppressLint
import android.app.Application
import android.bluetooth.BluetoothAdapter
import android.bluetooth.BluetoothDevice
import android.bluetooth.BluetoothManager
import android.content.Context
import androidx.annotation.StringRes
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import com.openpeats.protocol.Command
import com.openpeats.protocol.Commands
import com.openpeats.protocol.EqBand
import com.openpeats.protocol.KeyMapping
import com.openpeats.protocol.Pair2
import com.openpeats.protocol.eqUsesV1
import com.openpeats.transport.RfcommClient
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

/** Candidate paired earbuds. */
data class PairedDevice(val name: String, val address: String)

data class DeviceState(
    val connected: Boolean = false,
    val connecting: Boolean = false,

    /** The link dropped on its own and the app is trying to get it back. */
    val reconnecting: Boolean = false,
    val busy: Boolean = false,
    val error: String? = null,

    val deviceName: String = "",
    val firmware: String = "",

    val batteryLeft: Int = 0,
    val batteryRight: Int = 0,
    val batteryCase: Int = 0,

    val ancMode: Int = 0,
    val ancLevel: Int = 0,
    val ancLevels: List<Pair2> = emptyList(),

    val transparencyLevel: Int = 0,
    val transparencyLevels: List<Pair2> = emptyList(),

    val gameMode: Boolean = false,
    val multipoint: Boolean = false,
    val ldac: Boolean = false,
    val leAudio: Boolean = false,
    val touchDisabled: Boolean = false,

    val promptVolume: Int = 0,
    val voicePack: Int = 0,
    val keyMappings: List<KeyMapping> = emptyList(),

    /** Features absent from this model, detected on refresh. */
    val unsupported: Set<String> = emptySet(),
)

class DeviceViewModel(app: Application) : AndroidViewModel(app) {

    private companion object {
        /** The earbuds take a few seconds to come back after a reboot. */
        const val RECONNECT_DELAY_MS = 3000L
        const val RECONNECT_ATTEMPTS = 10
    }

    private val _state = MutableStateFlow(DeviceState())
    val state: StateFlow<DeviceState> = _state.asStateFlow()

    private val _log = MutableStateFlow<List<String>>(emptyList())
    val log: StateFlow<List<String>> = _log.asStateFlow()

    private var client: RfcommClient? = null

    /**
     * Scope of the current connection. Every exchange runs in it, so closing
     * the link — by hand or because it dropped — cancels them all: none of
     * them can then write a stale reply into the state that was just reset.
     */
    private var session: CoroutineScope? = null

    private var connectJob: Job? = null

    /**
     * Address to reconnect to when the link drops on its own. Cleared by an
     * explicit disconnect: automatic reconnection must not fight a user who
     * just tapped "Close".
     */
    private var wantedAddress: String? = null

    private fun adapter(): BluetoothAdapter? =
        (getApplication<Application>()
            .getSystemService(Context.BLUETOOTH_SERVICE) as? BluetoothManager)?.adapter

    /**
     * Resolves a string resource. Every user-facing string in this ViewModel
     * goes through here rather than being written inline, so the log and the
     * error banner follow the system language like the rest of the UI.
     */
    private fun str(@StringRes id: Int, vararg args: Any): String =
        getApplication<Application>().getString(id, *args)

    private fun logLine(s: String) {
        _log.update { (it + s).takeLast(200) }
    }

    /** Whether the Bluetooth adapter exists and is switched on. */
    fun bluetoothEnabled(): Boolean = adapter()?.isEnabled == true

    /**
     * Paired earbuds. Deliberately not filtered by name: other SoundPEATS
     * models share this protocol, and missing features are detected on
     * connection anyway.
     */
    @SuppressLint("MissingPermission")
    fun pairedDevices(): List<PairedDevice> =
        adapter()?.bondedDevices.orEmpty()
            .filter { it.bluetoothClass?.majorDeviceClass == 0x400 } // AUDIO_VIDEO
            .map { PairedDevice(it.name ?: it.address, it.address) }

    // ----------------------------------------------------------- connection

    @SuppressLint("MissingPermission")
    fun connect(address: String) {
        val dev: BluetoothDevice = adapter()?.getRemoteDevice(address) ?: run {
            _state.update { it.copy(error = str(R.string.error_bluetooth_unavailable)) }
            return
        }

        // A manual connection supersedes a reconnection still running.
        connectJob?.cancel()
        connectJob = viewModelScope.launch {
            _state.update { it.copy(connecting = true, reconnecting = false, error = null) }
            try {
                open(dev, address)
                logLine(str(R.string.log_connected, dev.name ?: address))
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                _state.update {
                    it.copy(
                        connecting = false,
                        connected = false,
                        error = describeError(e),
                    )
                }
                logLine(str(R.string.log_failed, e.message.orEmpty()))
            }
        }
    }

    /** Opens the link, starts a session on it and reads the state back. */
    @SuppressLint("MissingPermission")
    private suspend fun open(dev: BluetoothDevice, address: String) {
        val c = RfcommClient.connect(dev) { lost ->
            // Called on the reader's thread: hop back to the main one.
            viewModelScope.launch { onLinkLost(lost) }
        }
        client = c
        val parent = viewModelScope.coroutineContext[Job]
        session = CoroutineScope(viewModelScope.coroutineContext + SupervisorJob(parent))
        wantedAddress = address
        _state.update {
            it.copy(
                connected = true,
                connecting = false,
                reconnecting = false,
                error = null,
                deviceName = dev.name ?: address,
            )
        }
        refresh()
    }

    private fun describeError(e: Exception): String {
        val m = e.message.orEmpty()
        return when {
            m.contains("busy", true) || m.contains("in use", true) ->
                str(R.string.error_channel_busy)
            m.contains("read failed", true) || m.contains("closed", true) ->
                str(R.string.error_refused)
            else -> str(R.string.error_generic, m)
        }
    }

    /** Cancels every exchange of the current connection and releases it. */
    private fun endSession() {
        session?.cancel()
        session = null
        client?.close()
        client = null
    }

    fun disconnect() {
        wantedAddress = null
        connectJob?.cancel()
        connectJob = null
        endSession()
        _state.update { DeviceState() }
        logLine(str(R.string.log_disconnected))
    }

    /**
     * The link dropped without being asked to.
     *
     * LDAC, multipoint and LE Audio all reboot the earbuds: the control channel
     * goes down and only comes back a few seconds later. Without this the
     * screen stayed "connected" on a dead socket, and the next refresh listed
     * every feature as absent from the model.
     */
    @SuppressLint("MissingPermission")
    private fun onLinkLost(lost: RfcommClient) {
        if (client !== lost) return // an older link, already replaced or closed
        endSession()

        val address = wantedAddress
        val dev = address?.let { adapter()?.getRemoteDevice(it) }
        _state.update { DeviceState(reconnecting = dev != null) }
        if (address == null || dev == null) {
            logLine(str(R.string.log_link_lost))
            return
        }

        logLine(str(R.string.log_link_lost_reconnecting))
        connectJob?.cancel()
        connectJob = viewModelScope.launch {
            for (attempt in 1..RECONNECT_ATTEMPTS) {
                delay(RECONNECT_DELAY_MS)
                try {
                    open(dev, address)
                    logLine(str(R.string.log_reconnected))
                    return@launch
                } catch (e: CancellationException) {
                    throw e
                } catch (e: Exception) {
                    logLine(
                        str(R.string.log_reconnect_attempt, attempt, RECONNECT_ATTEMPTS, describeError(e))
                    )
                }
            }
            _state.update { it.copy(reconnecting = false) }
            logLine(str(R.string.log_reconnect_gave_up, RECONNECT_ATTEMPTS))
        }
    }

    /**
     * Runs one exchange in the current session. Nothing happens when there is
     * no connection; the exchange is cancelled if the link goes away.
     */
    private fun exchange(block: suspend CoroutineScope.(RfcommClient) -> Unit) {
        val c = client ?: return
        val s = session ?: return
        s.launch { block(c) }
    }

    // ---------------------------------------------------------------- reads

    fun refresh() = exchange { c ->
        _state.update { it.copy(busy = true) }
        val absent = mutableSetOf<String>()

        suspend fun scalar(key: String, cmd: Command): Int? {
            val f = c.get(cmd) ?: run { absent += key; return null }
            return f.scalar()
        }

        scalar("batteryLeft", Commands.BATTERY_LEFT)
            ?.let { v -> _state.update { it.copy(batteryLeft = v) } }
        scalar("batteryRight", Commands.BATTERY_RIGHT)
            ?.let { v -> _state.update { it.copy(batteryRight = v) } }
        scalar("batteryCase", Commands.BATTERY_CASE)
            ?.let { v -> _state.update { it.copy(batteryCase = v) } }

        c.get(Commands.FIRMWARE)?.let { f ->
            _state.update { it.copy(firmware = f.firmwareString()) }
        } ?: run { absent += "firmware" }

        scalar("ancMode", Commands.ANC_MODE_GET)
            ?.let { v -> _state.update { it.copy(ancMode = v) } }

        c.get(Commands.ANC_LEVEL_GET)?.let { f ->
            _state.update {
                it.copy(
                    ancLevels = f.pairs(),
                    ancLevel = (f.activeCode()?.toInt() ?: 0) and 0xFF,
                )
            }
        } ?: run { absent += "ancLevel" }

        c.get(Commands.TRANSPARENT_LEVEL_GET)?.let { f ->
            _state.update {
                it.copy(
                    transparencyLevels = f.pairs(),
                    transparencyLevel = (f.activeCode()?.toInt() ?: 0) and 0xFF,
                )
            }
        } ?: run { absent += "transparencyLevel" }

        scalar("gameMode", Commands.GAME_MODE_GET)
            ?.let { v -> _state.update { it.copy(gameMode = v == 1) } }
        scalar("multipoint", Commands.MULTIPOINT_GET)
            ?.let { v -> _state.update { it.copy(multipoint = v == 1) } }
        scalar("ldac", Commands.LDAC_GET)
            ?.let { v -> _state.update { it.copy(ldac = v == 1) } }
        scalar("leAudio", Commands.LE_AUDIO_GET)
            ?.let { v -> _state.update { it.copy(leAudio = v == 1) } }
        scalar("touchDisabled", Commands.TOUCH_DISABLE_GET)
            ?.let { v -> _state.update { it.copy(touchDisabled = v == 1) } }
        scalar("promptVolume", Commands.PROMPT_VOLUME_GET)
            ?.let { v -> _state.update { it.copy(promptVolume = v) } }
        scalar("voicePack", Commands.VOICE_PACK_GET)
            ?.let { v -> _state.update { it.copy(voicePack = v) } }

        c.get(Commands.CUSTOM_KEY_GET)?.let { f ->
            _state.update { it.copy(keyMappings = f.keyMappings()) }
        } ?: run { absent += "keyMappings" }

        // A link that dropped mid-refresh answers nothing: that says nothing
        // about the model, so the support map is left as it was.
        if (!c.isOpen) return@exchange

        _state.update { it.copy(busy = false, unsupported = absent) }
        logLine(str(R.string.log_refreshed, absent.size))
    }

    // --------------------------------------------------------------- writes
    //
    // Every write is followed by a read back: several `set` commands take
    // effect without acknowledging, and resending one would replay the side
    // effect.

    private fun writeThenRead(
        setCmd: Command,
        getCmd: Command,
        payload: ByteArray,
        settleMs: Long = 400,
        apply: (Int) -> Unit,
    ) = exchange { c ->
        _state.update { it.copy(busy = true) }
        c.set(setCmd, getCmd, payload)
        delay(settleMs)
        c.get(getCmd)?.let { apply(it.scalar()) }
        _state.update { it.copy(busy = false) }
    }

    fun setAncMode(mode: Int) =
        writeThenRead(Commands.ANC_MODE_SET, Commands.ANC_MODE_GET, byteArrayOf(mode.toByte())) { v ->
            _state.update { it.copy(ancMode = v) }
        }

    fun setAncLevel(code: Int) = exchange { c ->
        _state.update { it.copy(busy = true) }
        c.set(Commands.ANC_LEVEL_SET, Commands.ANC_LEVEL_GET, byteArrayOf(code.toByte()))
        delay(400)
        c.get(Commands.ANC_LEVEL_GET)?.let { f ->
            _state.update {
                it.copy(
                    ancLevels = f.pairs(),
                    ancLevel = (f.activeCode()?.toInt() ?: 0) and 0xFF,
                )
            }
        }
        _state.update { it.copy(busy = false) }
    }

    fun setTransparencyLevel(code: Int) = exchange { c ->
        _state.update { it.copy(busy = true) }
        c.set(
            Commands.TRANSPARENT_LEVEL_SET,
            Commands.TRANSPARENT_LEVEL_GET,
            byteArrayOf(code.toByte()),
        )
        delay(400)
        c.get(Commands.TRANSPARENT_LEVEL_GET)?.let { f ->
            _state.update {
                it.copy(
                    transparencyLevels = f.pairs(),
                    transparencyLevel = (f.activeCode()?.toInt() ?: 0) and 0xFF,
                )
            }
        }
        _state.update { it.copy(busy = false) }
    }

    fun setGameMode(on: Boolean) =
        writeThenRead(Commands.GAME_MODE_SET, Commands.GAME_MODE_GET, byteArrayOf(if (on) 1 else 0)) { v ->
            _state.update { it.copy(gameMode = v == 1) }
        }

    fun setLeAudio(on: Boolean) =
        writeThenRead(Commands.LE_AUDIO_SET, Commands.LE_AUDIO_GET, byteArrayOf(if (on) 1 else 0)) { v ->
            _state.update { it.copy(leAudio = v == 1) }
        }

    fun setTouchDisabled(on: Boolean) =
        writeThenRead(
            Commands.TOUCH_DISABLE_SET,
            Commands.TOUCH_DISABLE_GET,
            byteArrayOf(if (on) 1 else 0),
        ) { v -> _state.update { it.copy(touchDisabled = v == 1) } }

    fun setVoicePack(code: Int) =
        writeThenRead(Commands.VOICE_PACK_SET, Commands.VOICE_PACK_GET, byteArrayOf(code.toByte())) { v ->
            _state.update { it.copy(voicePack = v) }
        }

    fun setPromptVolume(v: Int) =
        writeThenRead(
            Commands.PROMPT_VOLUME_SET,
            Commands.PROMPT_VOLUME_GET,
            byteArrayOf(v.toByte()),
        ) { r -> _state.update { it.copy(promptVolume = r) } }

    /**
     * LDAC and multipoint are exclusive: the firmware drops one when the other
     * comes on, for lack of bandwidth over two links. Both are therefore read
     * back, after a longer delay because the links get reconfigured.
     */
    fun setMultipoint(on: Boolean) = exchange { c ->
        _state.update { it.copy(busy = true) }
        if (on) logLine(str(R.string.log_multipoint_enabling))
        c.setBool(Commands.MULTIPOINT_SET, Commands.MULTIPOINT_GET, on)
        delay(1200)
        c.get(Commands.MULTIPOINT_GET)?.let { f ->
            _state.update { it.copy(multipoint = f.scalar() == 1) }
        }
        c.get(Commands.LDAC_GET)?.let { f ->
            _state.update { it.copy(ldac = f.scalar() == 1) }
        }
        _state.update { it.copy(busy = false) }
    }

    fun setLdac(on: Boolean) = exchange { c ->
        _state.update { it.copy(busy = true) }
        if (on) logLine(str(R.string.log_ldac_enabling))
        c.setBool(Commands.LDAC_SET, Commands.LDAC_GET, on)
        delay(1200)
        c.get(Commands.LDAC_GET)?.let { f ->
            _state.update { it.copy(ldac = f.scalar() == 1) }
        }
        c.get(Commands.MULTIPOINT_GET)?.let { f ->
            _state.update { it.copy(multipoint = f.scalar() == 1) }
        }
        _state.update { it.copy(busy = false) }
    }

    fun applyEq(bands: List<EqBand>, masterGain: Double) = exchange { c ->
        _state.update { it.copy(busy = true) }
        c.applyEq(bands, masterGain, eqUsesV1(_state.value.firmware))
        logLine(str(R.string.log_eq_applied, bands.size))
        _state.update { it.copy(busy = false) }
    }

    override fun onCleared() {
        wantedAddress = null
        endSession()
        super.onCleared()
    }
}
