package com.openpeats.session

import com.openpeats.protocol.AncMode
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class DeviceStateTest {
    @Test
    fun theCycleGoesThroughTheThreeModes() {
        val off = AncMode.OFF.toInt()
        val on = AncMode.ON.toInt()
        val transparency = AncMode.TRANSPARENCY.toInt()
        assertEquals(on, nextAncMode(off))
        assertEquals(transparency, nextAncMode(on))
        assertEquals(off, nextAncMode(transparency))
    }

    @Test
    fun aModeOutsideTheCycleIsNotAPrison() {
        assertEquals(AncMode.ON.toInt(), nextAncMode(AncMode.ADAPTIVE.toInt()))
    }

    @Test
    fun knownEarbudsSurviveARoundTrip() {
        val k = KnownEarbuds(
            address = "98:80:BB:40:D4:ED",
            name = "SOUNDPEATS H3",
            batteryLeft = 80,
            batteryRight = 75,
            batteryCase = 60,
            ancMode = 2,
            ancLevel = 3,
            ancLevels = listOf(1, 2, 3),
            transparencyLevel = 1,
            transparencyLevels = listOf(0, 1),
            gameMode = true,
            touchDisabled = true,
            unsupported = setOf("ldac", "voicePack"),
            readAtMs = 1_700_000_000_000,
        )
        assertEquals(k, KnownEarbuds.fromJson(k.toJson()))
    }

    @Test
    fun anUnreadableCacheIsJustAbsent() {
        assertNull(KnownEarbuds.fromJson(null))
        assertNull(KnownEarbuds.fromJson(""))
        assertNull(KnownEarbuds.fromJson("{not json"))
        assertNull(KnownEarbuds.fromJson("{}"))
    }

    @Test
    fun theViewShowsTheModeOnItsWayFirst() {
        val known = KnownEarbuds("a", "n", ancMode = 0)
        assertEquals(0, EarbudsView(known = known).shownMode)
        assertEquals(2, EarbudsView(known = known, pendingMode = 2).shownMode)
        assertNull(EarbudsView().shownMode)
    }

    @Test
    fun anAlertFiresOnceThenAgainAfterRecovering() {
        val a = BatteryAlerts(threshold = 20)
        assertFalse(a.shouldAlert("left", 21))
        assertTrue(a.shouldAlert("left", 20))
        assertFalse("already raised", a.shouldAlert("left", 19))
        assertFalse("within the hysteresis", a.shouldAlert("left", 25))
        assertFalse("still raised", a.shouldAlert("left", 20))
        assertFalse("recovered", a.shouldAlert("left", 26))
        assertTrue("falls again", a.shouldAlert("left", 20))
    }

    @Test
    fun sourcesAlertIndependently() {
        val a = BatteryAlerts()
        assertTrue(a.shouldAlert("left", 10))
        assertTrue(a.shouldAlert("right", 10))
    }

    /** A case out of reach reads 0, which is not an empty battery. */
    @Test
    fun noReadingIsNotALowBattery() {
        val a = BatteryAlerts()
        assertFalse(a.shouldAlert("case", 0))
        assertFalse(a.shouldAlert("case", 200))
    }
}
