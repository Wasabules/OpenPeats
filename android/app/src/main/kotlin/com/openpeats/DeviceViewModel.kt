package com.openpeats

import android.annotation.SuppressLint
import android.app.Application
import android.bluetooth.BluetoothAdapter
import android.bluetooth.BluetoothDevice
import android.bluetooth.BluetoothManager
import android.content.Context
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import com.openpeats.protocol.Command
import com.openpeats.protocol.Commands
import com.openpeats.protocol.EqBand
import com.openpeats.protocol.KeyMapping
import com.openpeats.protocol.Pair2
import com.openpeats.transport.RfcommClient
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

/** Écouteurs appairés candidats. */
data class PairedDevice(val name: String, val address: String)

data class DeviceState(
    val connected: Boolean = false,
    val connecting: Boolean = false,
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

    /** Fonctions absentes de ce modèle, détectées au rafraîchissement. */
    val unsupported: Set<String> = emptySet(),
)

class DeviceViewModel(app: Application) : AndroidViewModel(app) {

    private val _state = MutableStateFlow(DeviceState())
    val state: StateFlow<DeviceState> = _state.asStateFlow()

    private val _log = MutableStateFlow<List<String>>(emptyList())
    val log: StateFlow<List<String>> = _log.asStateFlow()

    private var client: RfcommClient? = null

    private fun adapter(): BluetoothAdapter? =
        (getApplication<Application>()
            .getSystemService(Context.BLUETOOTH_SERVICE) as? BluetoothManager)?.adapter

    private fun logLine(s: String) {
        _log.update { (it + s).takeLast(200) }
    }

    /**
     * Écouteurs appairés. On ne filtre pas sur le nom : d'autres modèles
     * SoundPEATS partagent ce protocole, et les fonctions absentes sont de
     * toute façon détectées à la connexion.
     */
    @SuppressLint("MissingPermission")
    fun pairedDevices(): List<PairedDevice> =
        adapter()?.bondedDevices.orEmpty()
            .filter { it.bluetoothClass?.majorDeviceClass == 0x400 } // AUDIO_VIDEO
            .map { PairedDevice(it.name ?: it.address, it.address) }

    @SuppressLint("MissingPermission")
    fun connect(address: String) {
        val dev: BluetoothDevice = adapter()?.getRemoteDevice(address) ?: run {
            _state.update { it.copy(error = "Bluetooth indisponible.") }
            return
        }

        viewModelScope.launch {
            _state.update { it.copy(connecting = true, error = null) }
            try {
                val c = RfcommClient.connect(dev)
                client = c
                _state.update {
                    it.copy(
                        connected = true,
                        connecting = false,
                        deviceName = dev.name ?: address,
                    )
                }
                logLine("Connecté à ${dev.name ?: address}.")
                refresh()
            } catch (e: Exception) {
                _state.update {
                    it.copy(
                        connecting = false,
                        connected = false,
                        error = describeError(e),
                    )
                }
                logLine("Échec : ${e.message}")
            }
        }
    }

    private fun describeError(e: Exception): String {
        val m = e.message.orEmpty()
        return when {
            m.contains("busy", true) || m.contains("in use", true) ->
                "Canal de contrôle occupé. Si le multipoint est actif, un autre " +
                    "appareil le monopolise — coupez-y le Bluetooth puis réessayez."
            m.contains("read failed", true) || m.contains("closed", true) ->
                "Les écouteurs ont refusé la connexion. Sortez-les de l'étui et réessayez."
            else -> "Connexion impossible : $m"
        }
    }

    fun disconnect() {
        client?.close()
        client = null
        _state.update { DeviceState() }
        logLine("Déconnecté.")
    }

    // ------------------------------------------------------------- lectures

    fun refresh() = viewModelScope.launch {
        val c = client ?: return@launch
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

        _state.update { it.copy(busy = false, unsupported = absent) }
        logLine("Rafraîchi. ${absent.size} fonction(s) absente(s) de ce modèle.")
    }

    // ------------------------------------------------------------ écritures
    //
    // Après chaque écriture on relit : plusieurs `set` prennent effet sans
    // acquitter, et réémettre rejouerait l'effet de bord.

    private fun writeThenRead(
        setCmd: Command,
        getCmd: Command,
        payload: ByteArray,
        settleMs: Long = 400,
        apply: (Int) -> Unit,
    ) = viewModelScope.launch {
        val c = client ?: return@launch
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

    fun setAncLevel(code: Int) = viewModelScope.launch {
        val c = client ?: return@launch
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

    fun setTransparencyLevel(code: Int) = viewModelScope.launch {
        val c = client ?: return@launch
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
     * LDAC et multipoint sont exclusifs : le firmware coupe l'un en activant
     * l'autre, faute de bande passante pour deux liens. On relit donc les deux,
     * après un délai plus long car les liens se reconfigurent.
     */
    fun setMultipoint(on: Boolean) = viewModelScope.launch {
        val c = client ?: return@launch
        _state.update { it.copy(busy = true) }
        if (on) logLine("Activation du multipoint — LDAC va être désactivé par le firmware.")
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

    fun setLdac(on: Boolean) = viewModelScope.launch {
        val c = client ?: return@launch
        _state.update { it.copy(busy = true) }
        if (on) logLine("Activation de LDAC — le multipoint sera désactivé (exclusifs).")
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

    fun applyEq(bands: List<EqBand>, masterGain: Double) = viewModelScope.launch {
        val c = client ?: return@launch
        _state.update { it.copy(busy = true) }
        c.applyEq(bands, masterGain, eqUsesV1())
        logLine("EQ appliqué : ${bands.size} bande(s).")
        _state.update { it.copy(busy = false) }
    }

    /** Reproduit needInParallel() de l'application officielle. */
    private fun eqUsesV1(): Boolean {
        val fw = _state.value.firmware
        return listOf(
            "CAPSULE3_PROV2", "CAPSULE3_PRO", "WINGS2", "SPACE_PRO",
            "AIR5_LLITE", "PearClipPro_Ldac", "S15", "S22",
        ).any { fw.startsWith(it) }
    }

    override fun onCleared() {
        client?.close()
        super.onCleared()
    }
}
