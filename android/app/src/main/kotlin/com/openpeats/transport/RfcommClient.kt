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
import kotlinx.coroutines.CancellationException
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
    private val onLinkLost: (RfcommClient) -> Unit,
) {
    companion object {
        /** A command sent too soon after the link comes up gets no reply. */
        private const val SETTLE_MS = 700L

        private const val TIMEOUT_MS = 1500L
        private const val READ_RETRIES = 3

        /** The SPP channel observed on the H3, for firmwares with no usable SDP record. */
        private const val FALLBACK_CHANNEL = 11

        /**
         * Opens the control connection.
         *
         * [onLinkLost] fires, on a background thread, when the link drops
         * without [close] having been called — typically because the earbuds
         * rebooted, which LDAC, multipoint and LE Audio all trigger. It is
         * passed here rather than set afterwards so that a drop during the
         * settle delay is not missed.
         *
         * @throws IOException if the earbuds are unreachable, or if the
         *   channel is already held by another device (multipoint on).
         */
        suspend fun connect(
            device: BluetoothDevice,
            onLinkLost: (RfcommClient) -> Unit,
        ): RfcommClient {
            // Set from the IO thread once the client exists, so a cancellation
            // landing as withContext returns can still release it: the result
            // is dropped then, and nobody else holds the socket.
            var created: RfcommClient? = null
            try {
                return withContext(Dispatchers.IO) {
                    val c = RfcommClient(openSocket(device), onLinkLost)
                    created = c
                    try {
                        c.startReader()
                        delay(SETTLE_MS)
                    } catch (e: Throwable) {
                        c.close()
                        throw e
                    }
                    c
                }
            } catch (e: CancellationException) {
                created?.close()
                throw e
            }
        }

        /**
         * Connects through the SPP UUID, then through the fixed channel if that
         * fails.
         *
         * The service lookup (SDP) runs inside `connect()`, not when the socket
         * is created, so the fallback has to wrap `connect()`. It only runs
         * after a failed connection: no session was established, so this is
         * not the close-then-reopen the earbuds refuse.
         */
        @SuppressLint("MissingPermission")
        private fun openSocket(device: BluetoothDevice): BluetoothSocket {
            val primary = device.createRfcommSocketToServiceRecord(UUID.fromString(Protocol.SPP_UUID))
            val primaryError = try {
                primary.connect()
                return primary
            } catch (e: IOException) {
                closeQuietly(primary)
                e
            }

            Log.w(TAG, "SPP connection failed, trying channel $FALLBACK_CHANNEL", primaryError)
            val fallback = try {
                fallbackSocket(device, FALLBACK_CHANNEL)
            } catch (e: Exception) {
                // Reflection refused: the first error is the one worth reporting.
                primaryError.addSuppressed(e)
                throw primaryError
            }
            try {
                fallback.connect()
                return fallback
            } catch (e: IOException) {
                closeQuietly(fallback)
                // The SPP attempt carries the meaningful cause (busy, refused).
                primaryError.addSuppressed(e)
                throw primaryError
            }
        }

        /** `createRfcommSocket(int)` is hidden: reached by reflection. */
        private fun fallbackSocket(device: BluetoothDevice, channel: Int): BluetoothSocket {
            val m = device.javaClass.getMethod("createRfcommSocket", Int::class.javaPrimitiveType)
            return m.invoke(device, channel) as BluetoothSocket
        }

        private fun closeQuietly(socket: BluetoothSocket) {
            try {
                socket.close()
            } catch (_: IOException) {
            }
        }
    }

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private val frames = Channel<Frame>(capacity = 64)
    private val exchangeLock = Mutex()

    @Volatile
    private var closed = false

    /** Set once the link dropped on its own; [close] still has to be called. */
    @Volatile
    private var lost = false

    /** False once the link is gone, whether it dropped or was closed. */
    val isOpen: Boolean get() = !closed && !lost

    /** Receives every frame, spontaneous notifications included. */
    var onFrame: ((Frame) -> Unit)? = null

    private fun startReader() {
        scope.launch {
            val buf = ByteArray(1024)
            var acc = ByteArray(0)

            try {
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
            } finally {
                // The reader only stops for good: either close() asked for it,
                // or the link is gone. In the second case, fail the exchanges
                // in flight now rather than after their timeout, and say so.
                if (!closed) {
                    lost = true
                    frames.close()
                    onLinkLost(this@RfcommClient)
                }
            }
        }
    }

    private fun drain() {
        while (frames.tryReceive().isSuccess) { /* empty the queue */ }
    }

    /**
     * Waits for a frame satisfying [match]. Null on timeout, and right away
     * once the link is gone: a closed channel will never deliver the reply,
     * and receiving from it would throw into the caller's scope.
     */
    private suspend fun await(match: (Frame) -> Boolean): Frame? =
        withTimeoutOrNull(TIMEOUT_MS) {
            while (true) {
                val f = frames.receiveCatching().getOrNull() ?: return@withTimeoutOrNull null
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
            if (!isOpen) return null
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
