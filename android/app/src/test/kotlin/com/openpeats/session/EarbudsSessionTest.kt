package com.openpeats.session

import com.openpeats.protocol.AncMode
import com.openpeats.protocol.Commands
import com.openpeats.transport.RfcommClient
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.newSingleThreadContext
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeout
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.IOException

/**
 * The session's rules, run against fake earbuds through the real client: the
 * ones that matter are about what goes out on the wire, not about the state.
 */
@OptIn(ExperimentalCoroutinesApi::class, kotlinx.coroutines.DelicateCoroutinesApi::class)
class EarbudsSessionTest {
    private val address = "98:80:BB:40:D4:ED"
    private val thread = newSingleThreadContext("session")
    private val scope = CoroutineScope(SupervisorJob() + thread)
    private val earbuds = FakeEarbuds()

    /** Whether the earbuds are reachable: out of their case, in other words. */
    @Volatile
    private var reachable = true

    @Volatile
    private var clock = 1000L

    @Volatile
    private var audioLink: Boolean? = true

    private val store = object : EarbudsStore {
        override var wanted: String? = null
        override var known: KnownEarbuds? = null
        override var keepConnected = true
        override var batteryAlerts = true
    }

    private val env = object : EarbudsEnv {
        override fun text(id: Int, vararg args: Any) = "text"
        override fun quantity(id: Int, count: Int, vararg args: Any) = "text"
        override fun deviceName(address: String) = "H3"
        override fun now() = clock
        override suspend fun audioLinkUp(address: String) = audioLink

        override suspend fun openLink(address: String, onLost: (RfcommClient) -> Unit): RfcommClient {
            if (!reachable) throw IOException("read failed")
            return RfcommClient.over(earbuds.open(), onLost, settleMs = 1, timeoutMs = 50)
        }
    }

    private val timing = EarbudsSession.Timing(
        reconnectDelayMs = 20,
        reconnectAttempts = 3,
        waitingBackoffMs = listOf(10, 20),
        pollMs = 60_000,
        pendingTtlMs = 30_000,
        idleMs = 3000,
        writeSettleMs = 5,
    )

    private fun session() = EarbudsSession(env, store, scope, timing)

    /** Calls in on the session's thread, where everything it does runs. */
    private fun <T> on(block: () -> T): T = runBlocking { withContext(thread) { block() } }

    private fun waitFor(what: String, condition: () -> Boolean) = runBlocking {
        try {
            withTimeout(5000) {
                while (!condition()) delay(5)
            }
        } catch (e: Exception) {
            throw AssertionError("timed out waiting for $what", e)
        }
    }

    private fun EarbudsSession.connectAndSettle() {
        on { connect(address) }
        // The read ends with the touch lock and the keys; the battery and mode
        // are the part the rules below look at.
        waitFor("the first read") { state.value.connected && !state.value.busy && known.value?.readAtMs != 0L }
    }

    @After
    fun tearDown() {
        scope.cancel()
        thread.close()
    }

    @Test
    fun aModeWriteGoesOutOnceAndIsReadBack() {
        val s = session()
        s.connectAndSettle()

        on { s.handle(EarbudsCommand.AncModeTo(AncMode.ON.toInt())) }
        waitFor("the read back") { s.state.value.ancMode == AncMode.ON.toInt() && !s.state.value.busy }

        assertEquals(1, earbuds.writes(Commands.ANC_MODE_SET))
        assertEquals(AncMode.ON.toInt(), s.known.value?.ancMode)
        waitFor("the view") { s.view.value.pendingMode == null }
    }

    @Test
    fun aDoubleTapDoesNotWriteTwice() {
        val s = session()
        s.connectAndSettle()

        on {
            s.handle(EarbudsCommand.AncModeTo(AncMode.ON.toInt()))
            s.handle(EarbudsCommand.AncModeTo(AncMode.ON.toInt()))
        }
        waitFor("the read back") { s.state.value.ancMode == AncMode.ON.toInt() && !s.state.value.busy }

        assertEquals(1, earbuds.writes(Commands.ANC_MODE_SET))
    }

    @Test
    fun theModeAlreadyOnIsNotWrittenAgain() {
        val s = session()
        s.connectAndSettle()
        val reads = earbuds.writes(Commands.ANC_MODE_GET)

        on { s.handle(EarbudsCommand.AncModeTo(AncMode.OFF.toInt())) }
        waitFor("the check") { earbuds.writes(Commands.ANC_MODE_GET) > reads && !s.state.value.busy }

        assertEquals(0, earbuds.writes(Commands.ANC_MODE_SET))
    }

    /** A gesture on the earbuds moved the mode: the state we hold is behind. */
    @Test
    fun aTapStillWritesWhenTheStateIsBehindTheEarbuds() {
        val s = session()
        s.connectAndSettle()
        earbuds.values[Commands.ANC_MODE_GET] = AncMode.TRANSPARENCY.toInt()

        on { s.handle(EarbudsCommand.AncModeTo(AncMode.OFF.toInt())) }
        waitFor("the write") { earbuds.writes(Commands.ANC_MODE_SET) == 1 && !s.state.value.busy }

        assertEquals(AncMode.OFF.toInt(), earbuds.values[Commands.ANC_MODE_GET])
    }

    @Test
    fun aTapWithNoLinkConnectsThenWritesOnce() {
        store.known = KnownEarbuds(address, "H3")
        val s = session()

        assertTrue(on { s.handle(EarbudsCommand.AncModeTo(AncMode.TRANSPARENCY.toInt())) })
        waitFor("the view") { s.view.value.shownMode == AncMode.TRANSPARENCY.toInt() }
        waitFor("the write") {
            s.state.value.ancMode == AncMode.TRANSPARENCY.toInt() && !s.state.value.busy
        }

        assertEquals(1, earbuds.writes(Commands.ANC_MODE_SET))
        assertEquals(address, s.wanted.value)
    }

    @Test
    fun earbudsPickedButNeverReachedAreStillKnown() {
        reachable = false
        val s = session()
        on { s.connect(address) }
        waitFor("the waiting state") { s.state.value.waiting }

        assertEquals(address, s.known.value?.address)
        assertEquals(0L, s.known.value?.readAtMs)
    }

    @Test
    fun aTapWithNoKnownEarbudsIsLeftToTheApp() {
        val s = session()
        assertFalse(on { s.handle(EarbudsCommand.CycleAncMode) })
        assertNull(s.wanted.value)
    }

    @Test
    fun aTapFromLongAgoIsDropped() {
        store.known = KnownEarbuds(address, "H3")
        reachable = false
        val s = session()
        on { s.handle(EarbudsCommand.AncModeTo(AncMode.ON.toInt())) }
        waitFor("the waiting state") { s.state.value.waiting }
        waitFor("the view to give up on the tap") { s.view.value.pendingMode == null }

        clock += timing.pendingTtlMs + 1
        reachable = true
        waitFor("the link") { s.state.value.connected && !s.state.value.busy && s.known.value?.readAtMs != 0L }

        assertEquals(0, earbuds.writes(Commands.ANC_MODE_SET))
    }

    /** The whole point of the kept link: nothing to do when the earbuds return. */
    @Test
    fun theLinkComesBackAfterTheEarbudsWentAway() {
        val s = session()
        s.connectAndSettle()

        reachable = false
        earbuds.drop()
        waitFor("the waiting state") { s.state.value.waiting }
        assertFalse(s.state.value.connected)
        assertEquals("what was read stays known", 80, s.known.value?.batteryLeft)

        reachable = true
        waitFor("the link") { s.state.value.connected }
        assertFalse(s.state.value.waiting)
    }

    /** With no audio link nothing goes out: an attempt would be radio for nothing. */
    @Test
    fun waitingDoesNotTryUntilTheAudioLinkIsUp() {
        audioLink = false
        reachable = false
        val s = session()
        on { s.connect(address) }
        waitFor("the waiting state") { s.state.value.waiting }

        reachable = true
        runBlocking { delay(100) }
        assertFalse("no attempt without an audio link", s.state.value.connected)

        on { s.onAudioLink(address, true) }
        waitFor("the link") { s.state.value.connected }
    }

    @Test
    fun anAudioLinkOfAnotherDeviceChangesNothing() {
        audioLink = false
        reachable = false
        val s = session()
        on { s.connect(address) }
        waitFor("the waiting state") { s.state.value.waiting }
        reachable = true

        on { s.onAudioLink("00:11:22:33:44:55", true) }
        runBlocking { delay(100) }

        assertFalse(s.state.value.connected)
    }

    @Test
    fun closingStopsEverythingForGood() {
        val s = session()
        s.connectAndSettle()

        on { s.stop() }
        assertNull(s.wanted.value)
        assertNull(store.wanted)
        assertFalse(s.state.value.connected)

        runBlocking { delay(100) }
        assertFalse("nothing brings it back", s.state.value.connected)
        assertEquals("the last values stay readable", 80, s.known.value?.batteryLeft)
    }

    @Test
    fun aKeptLinkResumesInANewProcess() {
        val first = session()
        first.connectAndSettle()

        val second = session()
        assertEquals(address, second.wanted.value)
        assertEquals(80, second.known.value?.batteryLeft)
        on { second.resume() }
        waitFor("the link") { second.state.value.connected }
    }

    @Test
    fun aClosedLinkDoesNotResume() {
        val s = session()
        s.connectAndSettle()
        on { s.stop() }

        val next = session()
        on { next.resume() }
        runBlocking { delay(50) }

        assertFalse(next.state.value.connected)
        assertFalse(next.state.value.connecting)
    }

    @Test
    fun aMissingFeatureIsDetectedAndRemembered() {
        earbuds.values.remove(Commands.GAME_MODE_GET)
        val s = session()
        s.connectAndSettle()
        waitFor("the detection") { "gameMode" in s.known.value!!.unsupported }

        assertTrue("batteryLeft" !in s.known.value!!.unsupported)
    }

    @Test
    fun onDemandTheLinkClosesAfterTheLastUseAndFreesTheChannel() {
        store.keepConnected = false
        store.known = KnownEarbuds(address, "H3")
        val s = session()

        assertTrue(on { s.handle(EarbudsCommand.AncModeTo(AncMode.ON.toInt())) })
        waitFor("the write") { earbuds.writes(Commands.ANC_MODE_SET) == 1 && !s.state.value.busy }
        assertTrue(s.state.value.connected)

        waitFor("the idle close") { !s.state.value.connected }
        assertNull("nothing keeps it wanted", s.wanted.value)
        assertFalse("nothing waits either", s.state.value.waiting)
        assertEquals(AncMode.ON.toInt(), s.known.value?.ancMode)
    }

    @Test
    fun onDemandTheAppOnScreenHoldsTheLink() {
        store.keepConnected = false
        val s = session()
        on { s.setScreenOpen(true) }
        s.connectAndSettle()

        runBlocking { delay(timing.idleMs + 500) }
        assertTrue(s.state.value.connected)

        on { s.setScreenOpen(false) }
        waitFor("the idle close") { !s.state.value.connected }
    }

    @Test
    fun onDemandAFailureDoesNotWaitForTheEarbuds() {
        store.keepConnected = false
        reachable = false
        val s = session()
        on { s.setScreenOpen(true) }
        on { s.connect(address) }

        waitFor("the attempt to end") {
            !s.state.value.connecting && s.wanted.value == null
        }
        assertFalse(s.state.value.waiting)
        assertTrue("the reason stays up", s.state.value.error != null)
    }

    @Test
    fun switchingToOnDemandEndsAWait() {
        reachable = false
        val s = session()
        on { s.connect(address) }
        waitFor("the waiting state") { s.state.value.waiting }

        on { s.setKeepConnected(false) }

        assertFalse(s.state.value.waiting)
        assertNull(s.wanted.value)
        assertFalse(store.keepConnected)
    }

    @Test
    fun switchingBackToPermanentKeepsTheLink() {
        store.keepConnected = false
        val s = session()
        s.connectAndSettle()

        on { s.setKeepConnected(true) }
        runBlocking { delay(timing.idleMs + 500) }

        assertTrue(s.state.value.connected)
    }

    @Test
    fun alertsTurnedOffStayQuietAndDoNotSwallowTheNextOne() {
        earbuds.values[Commands.BATTERY_LEFT] = 15
        store.batteryAlerts = false
        val s = session()
        val seen = mutableListOf<LowBattery>()
        val job = scope.launch(start = CoroutineStart.UNDISPATCHED) {
            s.lowBattery.collect { synchronized(seen) { seen += it } }
        }
        s.connectAndSettle()
        assertTrue(seen.isEmpty())

        on {
            s.setBatteryAlerts(true)
            s.refresh()
        }
        waitFor("the alert") { seen.isNotEmpty() }
        job.cancel()
    }

    @Test
    fun aLowBatteryIsReported() {
        earbuds.values[Commands.BATTERY_LEFT] = 15
        val s = session()
        val seen = mutableListOf<LowBattery>()
        val job = scope.launch(start = CoroutineStart.UNDISPATCHED) {
            s.lowBattery.collect { synchronized(seen) { seen += it } }
        }
        s.connectAndSettle()
        waitFor("the alert") { seen.isNotEmpty() }
        job.cancel()

        assertEquals(LowBattery("batteryLeft", 15), seen.single())
    }
}
