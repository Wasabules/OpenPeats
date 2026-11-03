package com.openpeats.session

import com.openpeats.protocol.AncMode
import com.openpeats.protocol.KeyMapping
import com.openpeats.protocol.Pair2
import org.json.JSONArray
import org.json.JSONException
import org.json.JSONObject

/** Candidate paired earbuds. */
data class PairedDevice(val name: String, val address: String)

/** What the link to the earbuds is doing right now. Reset when it goes away. */
data class DeviceState(
    val connected: Boolean = false,
    val connecting: Boolean = false,

    /** The link dropped on its own and the app is trying to get it back. */
    val reconnecting: Boolean = false,

    /**
     * Attempts are over for now: the app connects again when the earbuds show
     * up, typically when they leave their case.
     */
    val waiting: Boolean = false,
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

/**
 * The last state read from the earbuds. Unlike [DeviceState] it outlives the
 * link and the process, so a widget has something true to show while the
 * earbuds sit in their case, labelled with the time it was read.
 */
data class KnownEarbuds(
    val address: String,
    val name: String,
    val batteryLeft: Int = 0,
    val batteryRight: Int = 0,
    val batteryCase: Int = 0,
    val ancMode: Int = 0,
    val ancLevel: Int = 0,
    val ancLevels: List<Int> = emptyList(),
    val transparencyLevel: Int = 0,
    val transparencyLevels: List<Int> = emptyList(),
    val gameMode: Boolean = false,
    val touchDisabled: Boolean = false,
    val unsupported: Set<String> = emptySet(),
    val readAtMs: Long = 0,
) {
    fun toJson(): String = JSONObject()
        .put("address", address)
        .put("name", name)
        .put("batteryLeft", batteryLeft)
        .put("batteryRight", batteryRight)
        .put("batteryCase", batteryCase)
        .put("ancMode", ancMode)
        .put("ancLevel", ancLevel)
        .put("ancLevels", JSONArray(ancLevels))
        .put("transparencyLevel", transparencyLevel)
        .put("transparencyLevels", JSONArray(transparencyLevels))
        .put("gameMode", gameMode)
        .put("touchDisabled", touchDisabled)
        .put("unsupported", JSONArray(unsupported.sorted()))
        .put("readAtMs", readAtMs)
        .toString()

    companion object {
        /** Null for anything unreadable: losing a cache must not stop the app. */
        fun fromJson(text: String?): KnownEarbuds? {
            if (text.isNullOrEmpty()) return null
            return try {
                val o = JSONObject(text)
                KnownEarbuds(
                    address = o.getString("address"),
                    name = o.getString("name"),
                    batteryLeft = o.optInt("batteryLeft"),
                    batteryRight = o.optInt("batteryRight"),
                    batteryCase = o.optInt("batteryCase"),
                    ancMode = o.optInt("ancMode"),
                    ancLevel = o.optInt("ancLevel"),
                    ancLevels = ints(o.optJSONArray("ancLevels")),
                    transparencyLevel = o.optInt("transparencyLevel"),
                    transparencyLevels = ints(o.optJSONArray("transparencyLevels")),
                    gameMode = o.optBoolean("gameMode"),
                    touchDisabled = o.optBoolean("touchDisabled"),
                    unsupported = strings(o.optJSONArray("unsupported")),
                    readAtMs = o.optLong("readAtMs"),
                )
            } catch (_: JSONException) {
                null
            }
        }

        private fun ints(a: JSONArray?): List<Int> =
            if (a == null) emptyList() else List(a.length()) { a.getInt(it) }

        private fun strings(a: JSONArray?): Set<String> =
            if (a == null) emptySet() else List(a.length()) { a.getString(it) }.toSet()
    }
}

enum class LinkStatus { IDLE, CONNECTING, CONNECTED, WAITING }

/** All a widget, the tile or the notification need to draw themselves. */
data class EarbudsView(
    val link: LinkStatus = LinkStatus.IDLE,
    val known: KnownEarbuds? = null,

    /** The ANC mode on its way to the earbuds, not read back yet. */
    val pendingMode: Int? = null,
) {
    /** What to light up: the mode being applied, or else the last one read. */
    val shownMode: Int? get() = pendingMode ?: known?.ancMode
}

/** The modes a one-button control goes through, in order. */
private val CYCLE = intArrayOf(AncMode.OFF.toInt(), AncMode.ON.toInt(), AncMode.TRANSPARENCY.toInt())

/**
 * The mode after [current]. A mode outside the cycle, which other models may
 * report, goes to noise cancelling rather than being stuck on itself.
 */
fun nextAncMode(current: Int): Int {
    val i = CYCLE.indexOf(current)
    return if (i < 0) AncMode.ON.toInt() else CYCLE[(i + 1) % CYCLE.size]
}

/**
 * Decides when a battery level deserves an alert: once on crossing the
 * threshold, then again only after climbing 5 points above it, so a level
 * hovering around the threshold does not keep alerting. The rule the desktop
 * app applies in battery.go.
 */
class BatteryAlerts(private val threshold: Int = 20) {
    private val raised = mutableSetOf<String>()

    fun shouldAlert(key: String, level: Int): Boolean {
        // 0 is what a source with no reading answers, such as a case that is
        // out of reach; a real empty battery would not be answering at all.
        if (level !in 1..100) return false
        if (level <= threshold) return raised.add(key)
        if (level > threshold + 5) raised.remove(key)
        return false
    }
}
