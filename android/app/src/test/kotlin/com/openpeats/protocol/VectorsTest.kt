package com.openpeats.protocol

import org.json.JSONArray
import org.json.JSONObject
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Test
import java.io.File

/**
 * The golden vectors shared with the Go implementation.
 *
 * They live at the repository root (testvectors/protocol.json) and Gradle
 * passes their path in. A case added there is checked against both
 * implementations at once — that is what keeps the two from drifting apart.
 */
class VectorsTest {

    private val vectors: JSONObject by lazy {
        val path = System.getProperty("openpeats.vectors")
        assertNotNull("openpeats.vectors is not set: run the tests through Gradle", path)
        JSONObject(File(path!!).readText())
    }

    private fun section(name: String): List<JSONObject> {
        val a = vectors.getJSONArray(name)
        return List(a.length()) { a.getJSONObject(it) }
    }

    /** Decodes a vector's hex string, spaces allowed. */
    private fun unhex(s: String): ByteArray {
        val h = s.replace(" ", "")
        require(h.length % 2 == 0) { "odd-length hex '$s' in the vectors" }
        return ByteArray(h.length / 2) { h.substring(2 * it, 2 * it + 2).toInt(16).toByte() }
    }

    private fun unhexByte(s: String): Byte = unhex(s).single()

    private fun unhexCmd(s: String): Command = unhex(s).let {
        require(it.size == 2) { "'$s' is not a two-byte opcode" }
        Command(it[0], it[1])
    }

    private fun hex(b: ByteArray): String = b.joinToString(" ") { "%02X".format(it) }

    private fun mappings(a: JSONArray): List<KeyMapping> = List(a.length()) {
        val m = a.getJSONObject(it)
        KeyMapping(unhexByte(m.getString("gesture")), unhexByte(m.getString("action")))
    }

    @Test
    fun encode() {
        for (c in section("encode")) {
            val got = Codec.encode(
                unhexCmd(c.getString("command")),
                unhex(c.getString("payload")),
                unhex(c.getString("vendor")),
            )
            assertEquals(c.getString("name"), hex(unhex(c.getString("frame"))), hex(got))
        }
    }

    @Test
    fun response() {
        for (c in section("response")) {
            assertEquals(
                c.getString("command"),
                unhexCmd(c.getString("response")),
                unhexCmd(c.getString("command")).response(),
            )
        }
    }

    /**
     * Feeds each stream through the same accumulate-and-decode loop as the
     * client's reader, so the vectors pin down what it actually keeps.
     */
    @Test
    fun decode() {
        for (c in section("decode")) {
            val name = c.getString("name")
            var acc = unhex(c.getString("stream"))
            val got = ArrayList<Frame>()
            while (true) {
                val d = Codec.decode(acc, acc.size)
                acc = acc.copyOfRange(d.consumed, acc.size)
                got += d.frame ?: break
            }

            val want = c.getJSONArray("frames")
            assertEquals("$name: frame count", want.length(), got.size)
            for (i in 0 until want.length()) {
                val w = want.getJSONObject(i)
                val g = got[i]
                assertEquals("$name: frame $i version", unhexByte(w.getString("version")), g.version)
                assertEquals("$name: frame $i flags", unhexByte(w.getString("flags")), g.flags)
                assertArrayEquals("$name: frame $i vendor", unhex(w.getString("vendor")), g.vendor)
                assertEquals("$name: frame $i command", unhexCmd(w.getString("command")), g.command)
                assertEquals("$name: frame $i payload", hex(unhex(w.getString("payload"))), hex(g.payload))
            }
            assertEquals("$name: left in the buffer", hex(unhex(c.getString("rest"))), hex(acc))
        }
    }

    private fun frameWith(payload: String) = Frame(
        version = Protocol.VERSION_RESPONSE,
        flags = Protocol.FLAGS,
        vendor = Protocol.VENDOR_DEFAULT,
        command = Command(0, 0),
        payload = unhex(payload),
    )

    @Test
    fun scalar() {
        for (c in section("scalar")) {
            assertEquals(c.getString("name"), c.getInt("value"), frameWith(c.getString("payload")).scalar())
        }
    }

    @Test
    fun pairs() {
        for (c in section("pairs")) {
            val name = c.getString("name")
            val f = frameWith(c.getString("payload"))
            val want = c.getJSONArray("pairs").let { a ->
                List(a.length()) {
                    val p = a.getJSONObject(it)
                    Pair2(unhexByte(p.getString("code")), p.getBoolean("active"))
                }
            }
            assertEquals(name, want, f.pairs())

            val active = if (c.isNull("active")) null else unhexByte(c.getString("active"))
            assertEquals("$name: active", active, f.activeCode())
        }
    }

    @Test
    fun keyMappings() {
        for (c in section("keyMappings")) {
            assertEquals(
                c.getString("name"),
                mappings(c.getJSONArray("mappings")),
                frameWith(c.getString("payload")).keyMappings(),
            )
        }
        for (c in section("encodeKeyMappings")) {
            assertEquals(
                c.getString("name"),
                hex(unhex(c.getString("payload"))),
                hex(Codec.encodeKeyMappings(mappings(c.getJSONArray("mappings")))),
            )
        }
    }

    @Test
    fun firmware() {
        for (c in section("firmware")) {
            assertEquals(c.getString("name"), c.getString("firmware"), frameWith(c.getString("payload")).firmwareString())
        }
    }

    @Test
    fun eqBand() {
        for (c in section("eqBand")) {
            val b = c.getJSONObject("band")
            val got = Codec.encodeEqBand(
                total = c.getInt("total"),
                index = c.getInt("index"),
                masterGain = c.getDouble("masterGain"),
                b = EqBand(b.getInt("freq"), b.getDouble("gain"), b.getDouble("q")),
                v1 = c.getBoolean("v1"),
            )
            assertEquals(c.getString("name"), hex(unhex(c.getString("payload"))), hex(got))
        }
    }

    @Test
    fun eqVersion() {
        for (c in section("eqUsesV1")) {
            val fw = c.getString("firmware")
            assertEquals("eqUsesV1(\"$fw\")", c.getBoolean("v1"), eqUsesV1(fw))
        }
    }
}
