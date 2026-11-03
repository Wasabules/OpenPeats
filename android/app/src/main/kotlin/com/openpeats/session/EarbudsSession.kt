package com.openpeats.session

import androidx.annotation.PluralsRes
import androidx.annotation.StringRes
import com.openpeats.R
import com.openpeats.protocol.AncMode
import com.openpeats.protocol.Command
import com.openpeats.protocol.Commands
import com.openpeats.protocol.EqBand
import com.openpeats.protocol.eqUsesV1
import com.openpeats.transport.RfcommClient
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import java.io.IOException

/** What the session needs from the platform, so a test can run without it. */
interface EarbudsEnv {
    fun text(@StringRes id: Int, vararg args: Any): String
    fun quantity(@PluralsRes id: Int, count: Int, vararg args: Any): String

    /** The system's name for these earbuds, or null if it has none. */
    fun deviceName(address: String): String?

    /** Opens the control channel. Throws [IOException] when that fails. */
    suspend fun openLink(address: String, onLost: (RfcommClient) -> Unit): RfcommClient

    /** Whether the phone has an audio link with them; null if it cannot tell. */
    suspend fun audioLinkUp(address: String): Boolean?

    fun now(): Long
}

/** What survives a restart. */
interface EarbudsStore {
    /** The earbuds to stay connected to. Null once the user closed the link. */
    var wanted: String?
    var known: KnownEarbuds?

    /** Hold the link for as long as possible, or only while it is being used. */
    var keepConnected: Boolean
    var batteryAlerts: Boolean
}

/** A low battery to tell the user about; [source] is a feature key. */
data class LowBattery(val source: String, val level: Int)

/** An action a widget, the tile, a shortcut or the notification asks for. */
sealed interface EarbudsCommand {
    data class AncModeTo(val mode: Int) : EarbudsCommand
    data object CycleAncMode : EarbudsCommand
    data class AncLevelTo(val code: Int) : EarbudsCommand
    data class TransparencyLevelTo(val code: Int) : EarbudsCommand
    data object ToggleGameMode : EarbudsCommand
    data object ToggleTouchLock : EarbudsCommand
}

/**
 * The link to the earbuds, for the whole process.
 *
 * It lives apart from any screen because a widget has to reach it with no
 * activity alive. By default it stays up until the user closes it: when the
 * earbuds go away, for a reboot or into their case, it waits and connects again
 * by itself. With [setKeepConnected] off it only lasts while it is being used,
 * which leaves the control channel free for another device the rest of the time.
 *
 * Everything here runs on the thread behind [scope], the main one in the app.
 */
class EarbudsSession(
    private val env: EarbudsEnv,
    private val store: EarbudsStore,
    private val scope: CoroutineScope,
    private val timing: Timing = Timing(),
) {
    /** The delays of the link. A test shortens them. */
    data class Timing(
        /** The earbuds take a few seconds to come back after a reboot. */
        val reconnectDelayMs: Long = 3000,
        val reconnectAttempts: Int = 10,

        /**
         * Between attempts once the phone's audio link is up. The first is
         * short: the audio profiles are still settling when that link appears.
         * The last is what keeps trying when another device holds the channel.
         */
        val waitingBackoffMs: List<Long> = listOf(2000, 5000, 15_000, 60_000),

        /** The rate the desktop app reads batteries at. */
        val pollMs: Long = 90_000,

        /**
         * How long a link kept only for use outlives the last command. Closing
         * right away would break the next tap: the earbuds refuse a reconnection
         * that follows a close too closely.
         */
        val idleMs: Long = 120_000,

        /** A tap older than this has lost its meaning by the time it could run. */
        val pendingTtlMs: Long = 30_000,

        /** Between a write and the read that checks it. */
        val writeSettleMs: Long = 400,
    )

    private val _state = MutableStateFlow(DeviceState())
    val state: StateFlow<DeviceState> = _state.asStateFlow()

    private val _log = MutableStateFlow<List<String>>(emptyList())
    val log: StateFlow<List<String>> = _log.asStateFlow()

    private val _wanted = MutableStateFlow(store.wanted)

    /** The earbuds the link is kept up for; null once the user closed it. */
    val wanted: StateFlow<String?> = _wanted.asStateFlow()

    private val _known = MutableStateFlow(store.known)
    val known: StateFlow<KnownEarbuds?> = _known.asStateFlow()

    private val _pendingMode = MutableStateFlow<Int?>(null)

    val view: StateFlow<EarbudsView> = combine(_state, _known, _pendingMode) { s, known, pending ->
        EarbudsView(
            link = when {
                s.connected -> LinkStatus.CONNECTED
                s.connecting || s.reconnecting -> LinkStatus.CONNECTING
                s.waiting -> LinkStatus.WAITING
                else -> LinkStatus.IDLE
            },
            known = known,
            pendingMode = pending,
        )
    }.stateIn(scope, SharingStarted.Eagerly, EarbudsView(known = store.known))

    private val _keepConnected = MutableStateFlow(store.keepConnected)
    val keepConnected: StateFlow<Boolean> = _keepConnected.asStateFlow()

    private val _batteryAlerts = MutableStateFlow(store.batteryAlerts)
    val batteryAlerts: StateFlow<Boolean> = _batteryAlerts.asStateFlow()

    private val _lowBattery = MutableSharedFlow<LowBattery>(extraBufferCapacity = 8)
    val lowBattery: SharedFlow<LowBattery> = _lowBattery

    private val alerts = BatteryAlerts()

    private var client: RfcommClient? = null

    /**
     * Scope of the current connection. Every exchange runs in it, so closing
     * the link — by hand or because it dropped — cancels them all: none of
     * them can then write a stale reply into the state that was just reset.
     */
    private var session: CoroutineScope? = null

    /** The attempts to get a link: the only job allowed to open one. */
    private var linkJob: Job? = null

    /** Counts down to closing a link kept only for use. */
    private var idleJob: Job? = null

    /** While the app is on screen a link is in use, whatever the last command was. */
    private var screenOpen = false

    /** Whether the phone has an audio link with the earbuds. See [waitForEarbuds]. */
    private val audioUp = MutableStateFlow(false)

    /** What to do once connected, for a tap that came with no link. */
    private class Pending(val atMs: Long, val run: () -> Unit)

    private var pending: Pending? = null

    /** The ANC mode being written, so a second tap does not write it again. */
    private var ancInFlight: Int? = null

    private fun logLine(s: String) {
        _log.update { (it + s).takeLast(200) }
    }

    // ------------------------------------------------------------ the link

    /** The user picked these earbuds: from now on the link is kept up for them. */
    fun connect(address: String) {
        setWanted(address)
        pending = null
        // Known from here on, reached or not: the widgets can then offer them
        // while they are still in their case.
        rememberEarbuds(address)
        startLink(address, afterLoss = false)
    }

    /**
     * Picks a kept link back up in a new process, after a reboot or a kill.
     * Does nothing if there is none, or if it is already being looked after.
     */
    fun resume() {
        val address = _wanted.value ?: return
        if (linkJob?.isActive == true || _state.value.connected) return
        startLink(address, afterLoss = false, quiet = true)
    }

    /** The user closed the link: nothing tries to get it back until asked. */
    fun stop() {
        closeLink()
        logLine(env.text(R.string.log_disconnected))
    }

    private fun closeLink() {
        setWanted(null)
        pending = null
        _pendingMode.value = null
        linkJob?.cancel()
        linkJob = null
        idleJob?.cancel()
        if (_state.value.connected) markReadNow()
        endSession()
        _state.value = DeviceState()
    }

    /**
     * Whether to hold the link for as long as possible. Turning this off also
     * ends a wait for the earbuds: nothing is in use any more.
     */
    fun setKeepConnected(on: Boolean) {
        _keepConnected.value = on
        store.keepConnected = on
        if (!on && _state.value.waiting) closeLink()
        restartIdle()
    }

    fun setBatteryAlerts(on: Boolean) {
        _batteryAlerts.value = on
        store.batteryAlerts = on
    }

    /** The app came on screen or left it. */
    fun setScreenOpen(open: Boolean) {
        screenOpen = open
        restartIdle()
    }

    private fun restartIdle() {
        idleJob?.cancel()
        if (_keepConnected.value || screenOpen || !_state.value.connected) return
        idleJob = scope.launch {
            delay(timing.idleMs)
            closeLink()
            logLine(env.text(R.string.log_idle_closed))
        }
    }

    /**
     * The phone's audio link with some device came up or went down. For the
     * earbuds we are kept for, that wakes [waitForEarbuds] up.
     */
    fun onAudioLink(address: String, up: Boolean) {
        if (address == _wanted.value) audioUp.value = up
    }

    private fun setWanted(address: String?) {
        _wanted.value = address
        store.wanted = address
    }

    private fun startLink(address: String, afterLoss: Boolean, quiet: Boolean = false) {
        // A new request supersedes the attempts still running.
        linkJob?.cancel()
        linkJob = scope.launch {
            if (afterLoss) {
                retryAfterLoss(address)
            } else {
                _state.update {
                    it.copy(connecting = true, reconnecting = false, waiting = false, error = null)
                }
                try {
                    open(address)
                    logLine(env.text(R.string.log_connected, _state.value.deviceName))
                } catch (e: CancellationException) {
                    throw e
                } catch (e: Exception) {
                    // Failing here is often just earbuds in their case. The
                    // error says what to do, and the link comes up once they
                    // are out, with no second tap.
                    if (!quiet) {
                        _state.update { it.copy(error = describeError(e)) }
                        logLine(env.text(R.string.log_failed, e.message.orEmpty()))
                    }
                }
            }
            if (_state.value.connected) return@launch
            if (_keepConnected.value) {
                waitForEarbuds(address)
            } else {
                // Not worth holding on for: the next use will try again. An
                // error stays up for whoever is looking at the screen.
                val error = _state.value.error
                closeLink()
                _state.value = DeviceState(error = error)
            }
        }
    }

    /** Brings the link back quickly, which covers the earbuds rebooting. */
    private suspend fun retryAfterLoss(address: String) {
        val attempts = timing.reconnectAttempts
        for (attempt in 1..attempts) {
            delay(timing.reconnectDelayMs)
            try {
                open(address)
                logLine(env.text(R.string.log_reconnected))
                return
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                logLine(env.text(R.string.log_reconnect_attempt, attempt, attempts, describeError(e)))
            }
        }
        logLine(env.quantity(R.plurals.log_reconnect_gave_up, attempts, attempts))
    }

    /**
     * Waits for the earbuds without touching the radio: an attempt only goes
     * out once the phone has an audio link with them, which is what taking them
     * out of their case brings up.
     */
    private suspend fun waitForEarbuds(address: String) {
        _state.update { it.copy(connecting = false, reconnecting = false, waiting = true) }
        // The tap keeps its chance to run for a moment, but with nothing in
        // flight the widgets stop showing it as being applied.
        _pendingMode.value = null
        audioUp.value = env.audioLinkUp(address) ?: true
        var step = 0
        while (true) {
            audioUp.first { it }
            delay(timing.waitingBackoffMs[step.coerceAtMost(timing.waitingBackoffMs.lastIndex)])
            step++
            if (!audioUp.value) {
                step = 0
                continue
            }
            try {
                open(address)
                logLine(env.text(R.string.log_reconnected))
                return
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                logLine(env.text(R.string.log_failed, describeError(e)))
            }
        }
    }

    /** Opens the link, starts a session on it and reads the state back. */
    private suspend fun open(address: String) {
        val c = env.openLink(address) { lost ->
            // Called on the reader's thread: hop back to the session's.
            scope.launch { onLinkLost(lost) }
        }
        // A link that dropped during the settle delay reported it before this
        // client was published, so onLinkLost ignored it as a stale one. Check
        // here, on the session's thread and with no suspension before
        // `client = c`: a drop is then either caught by this test or seen by
        // onLinkLost as the current client — never missed by both. The Go
        // client does the same with Alive().
        if (!c.isOpen) {
            c.close()
            throw IOException("link closed while settling")
        }
        client = c
        val s = CoroutineScope(scope.coroutineContext + SupervisorJob(scope.coroutineContext[Job]))
        session = s
        val name = env.deviceName(address) ?: address
        _state.update {
            it.copy(
                connected = true,
                connecting = false,
                reconnecting = false,
                waiting = false,
                error = null,
                deviceName = name,
            )
        }
        rememberEarbuds(address)

        s.launch {
            readAll(c)
            runPending()
        }
        s.launch { pollLoop(c) }
        restartIdle()
    }

    /** Cancels every exchange of the current connection and releases it. */
    private fun endSession() {
        session?.cancel()
        session = null
        client?.close()
        client = null
        ancInFlight = null
    }

    /**
     * The link dropped without being asked to.
     *
     * LDAC, multipoint and LE Audio all reboot the earbuds: the control channel
     * goes down and only comes back a few seconds later. Without this the
     * screen stayed "connected" on a dead socket, and the next refresh listed
     * every feature as absent from the model.
     */
    private fun onLinkLost(lost: RfcommClient) {
        if (client !== lost) return // an older link, already replaced or closed
        endSession()
        _pendingMode.value = null
        markReadNow()

        val address = _wanted.value
        _state.value = DeviceState(reconnecting = address != null)
        if (address == null) {
            logLine(env.text(R.string.log_link_lost))
            return
        }
        logLine(env.text(R.string.log_link_lost_reconnecting))
        startLink(address, afterLoss = true)
    }

    private fun describeError(e: Exception): String {
        val m = e.message.orEmpty()
        return when {
            e is NoBluetoothException -> env.text(R.string.error_bluetooth_unavailable)
            m.contains("busy", true) || m.contains("in use", true) ->
                env.text(R.string.error_channel_busy)
            m.contains("read failed", true) || m.contains("closed", true) ->
                env.text(R.string.error_refused)
            else -> env.text(R.string.error_generic, m)
        }
    }

    /**
     * Runs one exchange in the current session. Nothing happens when there is
     * no connection; the exchange is cancelled if the link goes away.
     */
    private fun exchange(block: suspend CoroutineScope.(RfcommClient) -> Unit) {
        val c = client ?: return
        val s = session ?: return
        restartIdle()
        s.launch { block(c) }
    }

    // ---------------------------------------------------- what is remembered

    /** Starts remembering these earbuds, unless they are the ones already remembered. */
    private fun rememberEarbuds(address: String) {
        val name = env.deviceName(address) ?: address
        _known.update {
            (it?.takeIf { k -> k.address == address } ?: KnownEarbuds(address, name)).copy(name = name)
        }
        persistKnown()
    }

    /** Copies the state just read into what outlives the link. */
    private fun rememberState() {
        val s = _state.value
        val before = _known.value ?: return
        val after = before.copy(
            batteryLeft = s.batteryLeft,
            batteryRight = s.batteryRight,
            batteryCase = s.batteryCase,
            ancMode = s.ancMode,
            ancLevel = s.ancLevel,
            ancLevels = s.ancLevels.map { it.code.toInt() and 0xFF },
            transparencyLevel = s.transparencyLevel,
            transparencyLevels = s.transparencyLevels.map { it.code.toInt() and 0xFF },
            gameMode = s.gameMode,
            touchDisabled = s.touchDisabled,
            unsupported = s.unsupported,
        )
        // The time alone does not count as a change: it is only ever shown
        // once the link is gone, so a poll reading the same values has nothing
        // to redraw on the widgets.
        if (after == before) return
        _known.value = after.copy(readAtMs = env.now())
        persistKnown()
    }

    /** The values were still true up to now: the link has just ended. */
    private fun markReadNow() {
        _known.update { it?.copy(readAtMs = env.now()) }
        persistKnown()
    }

    private fun persistKnown() {
        store.known = _known.value
    }

    // ---------------------------------------------------------------- reads

    fun refresh() = exchange { c -> readAll(c) }

    private suspend fun readAll(c: RfcommClient) {
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
        if (!c.isOpen) return

        _state.update { it.copy(busy = false, unsupported = absent) }
        rememberState()
        val s = _state.value
        checkBattery("batteryLeft", s.batteryLeft)
        checkBattery("batteryRight", s.batteryRight)
        checkBattery("batteryCase", s.batteryCase)
        logLine(env.quantity(R.plurals.log_refreshed, absent.size, absent.size))
    }

    /**
     * Keeps the batteries current, and the mode with them: it also changes from
     * a gesture on the earbuds, which a widget must not keep contradicting.
     */
    private suspend fun pollLoop(c: RfcommClient) {
        while (true) {
            delay(timing.pollMs)
            val absent = _state.value.unsupported
            suspend fun level(key: String, cmd: Command, set: DeviceState.(Int) -> DeviceState) {
                if (key in absent) return
                val v = c.get(cmd)?.scalar() ?: return
                if (v > 100) return // out-of-range value: ignore rather than alert wrongly
                _state.update { it.set(v) }
                checkBattery(key, v)
            }
            level("batteryLeft", Commands.BATTERY_LEFT) { copy(batteryLeft = it) }
            level("batteryRight", Commands.BATTERY_RIGHT) { copy(batteryRight = it) }
            level("batteryCase", Commands.BATTERY_CASE) { copy(batteryCase = it) }
            if ("ancMode" !in absent && ancInFlight == null) {
                c.get(Commands.ANC_MODE_GET)?.let { f ->
                    // Checked again: a tap may have started during the exchange.
                    if (ancInFlight == null) _state.update { it.copy(ancMode = f.scalar()) }
                }
            }
            rememberState()
        }
    }

    private fun checkBattery(key: String, level: Int) {
        // Not even evaluated when off: marking an alert as raised here would
        // swallow it for the rest of the discharge once they are back on.
        if (!_batteryAlerts.value || key in _state.value.unsupported) return
        if (alerts.shouldAlert(key, level)) _lowBattery.tryEmit(LowBattery(key, level))
    }

    // ------------------------------------------- commands from outside the app

    /**
     * Runs a command from a widget, the tile, a shortcut or the notification.
     * With no link it gets one first. False when no earbuds are known yet, which
     * only the app can sort out.
     */
    fun handle(command: EarbudsCommand): Boolean {
        val address = _wanted.value ?: _known.value?.address ?: return false
        if (_state.value.connected) {
            run(command)
            return true
        }
        val mode = when (command) {
            is EarbudsCommand.AncModeTo -> command.mode
            EarbudsCommand.CycleAncMode -> nextAncMode(_known.value?.ancMode ?: AncMode.OFF.toInt())
            else -> null
        }
        _pendingMode.value = mode
        pending = Pending(env.now()) {
            // Resolved against what was just read, not against an old state.
            run(if (mode != null) EarbudsCommand.AncModeTo(mode) else command)
        }
        setWanted(address)
        // Attempts already in flight will pick the tap up. In the waiting state
        // nothing is going out, and the user is plainly asking for it now.
        if (_state.value.waiting || linkJob?.isActive != true) {
            startLink(address, afterLoss = false, quiet = true)
        }
        return true
    }

    private fun run(command: EarbudsCommand) {
        val s = _state.value
        when (command) {
            is EarbudsCommand.AncModeTo -> requestAncMode(command.mode)
            EarbudsCommand.CycleAncMode -> requestAncMode(nextAncMode(ancInFlight ?: s.ancMode))
            is EarbudsCommand.AncLevelTo -> setAncLevel(command.code)
            is EarbudsCommand.TransparencyLevelTo -> setTransparencyLevel(command.code)
            EarbudsCommand.ToggleGameMode -> setGameMode(!s.gameMode)
            EarbudsCommand.ToggleTouchLock -> setTouchDisabled(!s.touchDisabled)
        }
    }

    private fun runPending() {
        val p = pending ?: return
        pending = null
        _pendingMode.value = null
        if (env.now() - p.atMs <= timing.pendingTtlMs) p.run()
    }

    /**
     * Asks for an ANC mode, at most once. A write replays its voice prompt, so
     * asking again for the mode being written, or for the one already on, must
     * not write. The state can be behind a gesture on the earbuds, which is why
     * the second case reads first instead of trusting it.
     */
    fun requestAncMode(mode: Int) {
        if (client == null || ancInFlight == mode) return
        val alreadyOn = ancInFlight == null && _state.value.ancMode == mode
        ancInFlight = mode
        _pendingMode.value = mode
        exchange { c ->
            _state.update { it.copy(busy = true) }
            val current = if (alreadyOn) c.get(Commands.ANC_MODE_GET)?.scalar() else null
            if (current != mode) {
                c.set(Commands.ANC_MODE_SET, Commands.ANC_MODE_GET, byteArrayOf(mode.toByte()))
                delay(timing.writeSettleMs)
            }
            val read = if (current == mode) mode else c.get(Commands.ANC_MODE_GET)?.scalar()
            // A later tap owns the display from here on.
            if (ancInFlight == mode) {
                ancInFlight = null
                _pendingMode.value = null
            }
            _state.update { it.copy(ancMode = read ?: it.ancMode, busy = false) }
            rememberState()
        }
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
        apply: (Int) -> Unit,
    ) = exchange { c ->
        _state.update { it.copy(busy = true) }
        c.set(setCmd, getCmd, payload)
        delay(timing.writeSettleMs)
        c.get(getCmd)?.let { apply(it.scalar()) }
        _state.update { it.copy(busy = false) }
        rememberState()
    }

    fun setAncLevel(code: Int) = exchange { c ->
        _state.update { it.copy(busy = true) }
        c.set(Commands.ANC_LEVEL_SET, Commands.ANC_LEVEL_GET, byteArrayOf(code.toByte()))
        delay(timing.writeSettleMs)
        c.get(Commands.ANC_LEVEL_GET)?.let { f ->
            _state.update {
                it.copy(
                    ancLevels = f.pairs(),
                    ancLevel = (f.activeCode()?.toInt() ?: 0) and 0xFF,
                )
            }
        }
        _state.update { it.copy(busy = false) }
        rememberState()
    }

    fun setTransparencyLevel(code: Int) = exchange { c ->
        _state.update { it.copy(busy = true) }
        c.set(
            Commands.TRANSPARENT_LEVEL_SET,
            Commands.TRANSPARENT_LEVEL_GET,
            byteArrayOf(code.toByte()),
        )
        delay(timing.writeSettleMs)
        c.get(Commands.TRANSPARENT_LEVEL_GET)?.let { f ->
            _state.update {
                it.copy(
                    transparencyLevels = f.pairs(),
                    transparencyLevel = (f.activeCode()?.toInt() ?: 0) and 0xFF,
                )
            }
        }
        _state.update { it.copy(busy = false) }
        rememberState()
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
        if (on) logLine(env.text(R.string.log_multipoint_enabling))
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
        if (on) logLine(env.text(R.string.log_ldac_enabling))
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
        logLine(env.quantity(R.plurals.log_eq_applied, bands.size, bands.size))
        _state.update { it.copy(busy = false) }
    }
}

/** The phone has no Bluetooth adapter to open a link with. */
class NoBluetoothException : IOException("no Bluetooth adapter")
