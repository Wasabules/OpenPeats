package com.openpeats.transport

import android.annotation.SuppressLint
import android.bluetooth.BluetoothDevice
import android.bluetooth.BluetoothSocket
import android.util.Log
import com.openpeats.protocol.Codec
import com.openpeats.protocol.Command
import com.openpeats.protocol.Commands
import com.openpeats.protocol.EqBand
import com.openpeats.protocol.Frame
import com.openpeats.protocol.Protocol
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeoutOrNull
import java.io.IOException
import java.util.UUID

private const val TAG = "OpenPeats"

/**
 * Control client over RFCOMM/SPP.
 *
 * The timings come from real measurements; see PROTOCOL.md. Android resolves
 * the RFCOMM channel itself from the SPP UUID, which avoids the sweep Linux
 * needs.
 */
class RfcommClient private constructor(
    private val socket: BluetoothSocket,
) {
    companion object {
        /** A command sent too soon after the link comes up gets no reply. */
        private const val SETTLE_MS = 700L

        private const val TIMEOUT_MS = 1500L
        private const val READ_RETRIES = 3

        /**
         * Opens the control connection.
         *
         * @throws IOException if the earbuds are unreachable, or if the
         *   channel is already held by another device (multipoint on).
         */
        @SuppressLint("MissingPermission")
        suspend fun connect(device: BluetoothDevice): RfcommClient =
            withContext(Dispatchers.IO) {
                val uuid = UUID.fromString(Protocol.SPP_UUID)
                val socket = try {
                    device.createRfcommSocketToServiceRecord(uuid)
                } catch (e: IOException) {
                    // Fallback: some firmwares expose no usable SDP record.
                    // Channel 11 is the one observed on the H3.
                    Log.w(TAG, "SDP unavailable, falling back to the fixed channel", e)
                    fallbackSocket(device, 11)
                }

                socket.connect()
                val c = RfcommClient(socket)
                c.startReader()
                delay(SETTLE_MS)
                c
            }

        /** `createRfcommSocket(int)` is hidden: reached by reflection. */
        private fun fallbackSocket(device: BluetoothDevice, channel: Int): BluetoothSocket {
            val m = device.javaClass.getMethod("createRfcommSocket", Int::class.javaPrimitiveType)
            return m.invoke(device, channel) as BluetoothSocket
        }
    }

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private val frames = Channel<Frame>(capacity = 64)
    private val exchangeLock = Mutex()

    @Volatile
    private var closed = false

    /** Receives every frame, spontaneous notifications included. */
    var onFrame: ((Frame) -> Unit)? = null

    private fun startReader() {
        scope.launch {
            val buf = ByteArray(1024)
            var acc = ByteArray(0)

            while (isActive && !closed) {
                val n = try {
                    socket.inputStream.read(buf)
                } catch (e: IOException) {
                    if (!closed) Log.w(TAG, "read interrupted", e)
                    break
                }
                if (n <= 0) break

                acc += buf.copyOf(n)
                while (true) {
                    val d = Codec.decode(acc, acc.size)
                    if (d.frame == null) {
                        // Noise before the SOF: drop it.
                        if (d.consumed > 0) acc = acc.copyOfRange(d.consumed, acc.size)
                        break
                    }
                    acc = acc.copyOfRange(d.consumed, acc.size)
                    onFrame?.invoke(d.frame)
                    frames.trySend(d.frame)
                }
            }
        }
    }

    private fun drain() {
        while (frames.tryReceive().isSuccess) { /* empty the queue */ }
    }

    private suspend fun await(match: (Frame) -> Boolean): Frame? =
        withTimeoutOrNull(TIMEOUT_MS) {
            while (true) {
                val f = frames.receive()
                if (match(f)) return@withTimeoutOrNull f
            }
            @Suppress("UNREACHABLE_CODE")
            null
        }

    private suspend fun write(b: ByteArray) = withContext(Dispatchers.IO) {
        socket.outputStream.write(b)
        socket.outputStream.flush()
    }

    /**
     * Reads a value. Reads are retried: they have no side effects.
     */
    suspend fun get(cmd: Command): Frame? = exchangeLock.withLock {
        val want = cmd.response()
        repeat(READ_RETRIES) {
            drain()
            try {
                write(Codec.encode(cmd))
            } catch (e: IOException) {
                Log.w(TAG, "write failed $cmd", e)
                return null
            }
            await { it.command == want }?.let { return it }
        }
        null
    }

    /**
     * Writes a value.
     *
     * ⚠ Never retried, ever. Several `set` commands take effect without
     * acknowledging (ANC mode, multipoint); resending one replays the side
     * effect — observed as three voice prompts for a single intended write.
     *
     * The acknowledgement generally carries the reply opcode of the matching
     * `get`. Its absence is not an error: read the state back.
     */
    suspend fun set(setCmd: Command, getCmd: Command, payload: ByteArray): Frame? =
        exchangeLock.withLock {
            drain()
            try {
                write(Codec.encode(setCmd, payload))
            } catch (e: IOException) {
                Log.w(TAG, "write failed $setCmd", e)
                return null
            }
            val want = getCmd.response()
            await { it.command == want }
        }

    suspend fun setBool(setCmd: Command, getCmd: Command, on: Boolean) {
        set(setCmd, getCmd, byteArrayOf(if (on) 1 else 0))
    }

    /**
     * Applies an equaliser curve, one frame per band.
     *
     * The EQ family uses its own vendor identifier and answers with
     * [Commands.EQ_RESPONSE], not with the "bit 7" convention.
     */
    suspend fun applyEq(bands: List<EqBand>, masterGain: Double, v1: Boolean) {
        if (bands.isEmpty()) return
        exchangeLock.withLock {
            val cmd = if (v1) Commands.EQ_V1 else Commands.EQ_V2
            bands.forEachIndexed { i, b ->
                drain()
                val payload = Codec.encodeEqBand(bands.size, i + 1, masterGain, b, v1)
                try {
                    write(Codec.encode(cmd, payload, Protocol.VENDOR_EQ))
                } catch (e: IOException) {
                    Log.w(TAG, "band ${i + 1}: write failed", e)
                    return
                }
                // The ack is brief; its absence does not stop the send.
                await { it.command == Commands.EQ_RESPONSE }
                delay(80)
            }
        }
    }

    fun close() {
        if (closed) return
        closed = true
        scope.cancel()
        try {
            socket.close()
        } catch (_: IOException) {
        }
        frames.close()
    }
}
