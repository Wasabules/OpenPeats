package com.openpeats.session

import com.openpeats.protocol.Codec
import com.openpeats.protocol.Command
import com.openpeats.protocol.Commands
import com.openpeats.protocol.Protocol
import com.openpeats.transport.ControlLink
import java.io.ByteArrayOutputStream
import java.io.IOException
import java.io.InputStream
import java.io.OutputStream
import java.io.PipedInputStream
import java.io.PipedOutputStream
import java.util.Collections

/**
 * Earbuds at the far end of a [ControlLink]: they answer reads, apply writes,
 * and record every frame they get. A key absent from [values] is a feature the
 * model does not have, which simply never answers.
 */
class FakeEarbuds(
    val values: MutableMap<Command, Int> = defaultValues(),
) {
    val received: MutableList<Command> = Collections.synchronizedList(mutableListOf())
    private var current: Link? = null

    fun writes(cmd: Command): Int = received.count { it == cmd }

    /** A new control channel, as a connection attempt that succeeds opens. */
    fun open(): ControlLink = Link().also { current = it }

    /** The earbuds go away without warning: they rebooted, or went in the case. */
    fun drop() {
        current?.close()
    }

    private inner class Link : ControlLink {
        private val toClient = PipedOutputStream()
        override val input: InputStream = PipedInputStream(toClient, 4096)

        override val output: OutputStream = object : ByteArrayOutputStream() {
            override fun flush() {
                val bytes = toByteArray()
                reset()
                val d = Codec.decode(bytes, bytes.size)
                d.frame?.let { respond(it.command, it.payload) }
            }
        }

        private fun respond(cmd: Command, payload: ByteArray) {
            received += cmd
            val set = SET_TO_GET[cmd]
            if (set != null) {
                if (set !in values) return
                values[set] = payload.last().toInt() and 0xFF
                // As on the wire: the ack carries the reply opcode of the get.
                reply(set.response(), byteArrayOf(0, values.getValue(set).toByte()))
                return
            }
            val v = values[cmd] ?: return
            reply(cmd.response(), byteArrayOf(0, v.toByte()))
        }

        private fun reply(cmd: Command, payload: ByteArray) {
            val frame = Codec.encode(cmd, payload)
            frame[1] = Protocol.VERSION_RESPONSE
            try {
                toClient.write(frame)
                toClient.flush()
            } catch (_: IOException) {
            }
        }

        override fun close() {
            try {
                toClient.close()
            } catch (_: IOException) {
            }
        }
    }

    companion object {
        private val SET_TO_GET = mapOf(
            Commands.ANC_MODE_SET to Commands.ANC_MODE_GET,
            Commands.GAME_MODE_SET to Commands.GAME_MODE_GET,
            Commands.TOUCH_DISABLE_SET to Commands.TOUCH_DISABLE_GET,
        )

        fun defaultValues(): MutableMap<Command, Int> = mutableMapOf(
            Commands.BATTERY_LEFT to 80,
            Commands.BATTERY_RIGHT to 75,
            Commands.BATTERY_CASE to 60,
            Commands.ANC_MODE_GET to 0,
            Commands.GAME_MODE_GET to 0,
            Commands.TOUCH_DISABLE_GET to 0,
        )
    }
}
