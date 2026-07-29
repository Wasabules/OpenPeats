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
 * Client de contrôle sur RFCOMM/SPP.
 *
 * Les réglages temporels viennent de mesures réelles ; voir PROTOCOL.md.
 * Android résout lui-même le canal RFCOMM à partir de l'UUID SPP, ce qui évite
 * le balayage nécessaire sous Linux.
 */
class RfcommClient private constructor(
    private val socket: BluetoothSocket,
) {
    companion object {
        /** Une commande émise trop tôt après le lien reste sans réponse. */
        private const val SETTLE_MS = 700L

        private const val TIMEOUT_MS = 1500L
        private const val READ_RETRIES = 3

        /**
         * Ouvre la connexion de contrôle.
         *
         * @throws IOException si les écouteurs sont injoignables, ou si le
         *   canal est déjà tenu par un autre appareil (multipoint actif).
         */
        @SuppressLint("MissingPermission")
        suspend fun connect(device: BluetoothDevice): RfcommClient =
            withContext(Dispatchers.IO) {
                val uuid = UUID.fromString(Protocol.SPP_UUID)
                val socket = try {
                    device.createRfcommSocketToServiceRecord(uuid)
                } catch (e: IOException) {
                    // Repli : certains firmwares n'exposent pas d'enregistrement
                    // SDP exploitable. Le canal 11 est celui observé sur le H3.
                    Log.w(TAG, "SDP indisponible, repli sur le canal fixe", e)
                    fallbackSocket(device, 11)
                }

                socket.connect()
                val c = RfcommClient(socket)
                c.startReader()
                delay(SETTLE_MS)
                c
            }

        /** `createRfcommSocket(int)` est masquée : accès par réflexion. */
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

    /** Reçoit toutes les trames, y compris les notifications spontanées. */
    var onFrame: ((Frame) -> Unit)? = null

    private fun startReader() {
        scope.launch {
            val buf = ByteArray(1024)
            var acc = ByteArray(0)

            while (isActive && !closed) {
                val n = try {
                    socket.inputStream.read(buf)
                } catch (e: IOException) {
                    if (!closed) Log.w(TAG, "lecture interrompue", e)
                    break
                }
                if (n <= 0) break

                acc += buf.copyOf(n)
                while (true) {
                    val d = Codec.decode(acc, acc.size)
                    if (d.frame == null) {
                        // Bruit avant le SOF : on le jette.
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
        while (frames.tryReceive().isSuccess) { /* vide la file */ }
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
     * Lit une valeur. Les lectures sont réessayées : elles sont sans effet
     * de bord.
     */
    suspend fun get(cmd: Command): Frame? = exchangeLock.withLock {
        val want = cmd.response()
        repeat(READ_RETRIES) {
            drain()
            try {
                write(Codec.encode(cmd))
            } catch (e: IOException) {
                Log.w(TAG, "échec d'écriture $cmd", e)
                return null
            }
            await { it.command == want }?.let { return it }
        }
        null
    }

    /**
     * Écrit une valeur.
     *
     * ⚠ Aucune réémission, jamais. Plusieurs `set` prennent effet sans
     * acquitter (mode ANC, multipoint) ; renvoyer la commande rejouerait
     * l'effet de bord — observé avec trois invites vocales pour une seule
     * écriture voulue.
     *
     * L'acquittement porte généralement l'opcode de réponse du `get`
     * correspondant. Son absence n'est pas une erreur : relire l'état.
     */
    suspend fun set(setCmd: Command, getCmd: Command, payload: ByteArray): Frame? =
        exchangeLock.withLock {
            drain()
            try {
                write(Codec.encode(setCmd, payload))
            } catch (e: IOException) {
                Log.w(TAG, "échec d'écriture $setCmd", e)
                return null
            }
            val want = getCmd.response()
            await { it.command == want }
        }

    suspend fun setBool(setCmd: Command, getCmd: Command, on: Boolean) {
        set(setCmd, getCmd, byteArrayOf(if (on) 1 else 0))
    }

    /**
     * Applique une courbe d'égaliseur, une trame par bande.
     *
     * La famille EQ utilise son propre identifiant vendeur et répond avec
     * [Commands.EQ_RESPONSE], pas avec la convention « bit 7 ».
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
                    Log.w(TAG, "bande ${i + 1} : échec d'écriture", e)
                    return
                }
                // L'ack est bref ; son absence n'interrompt pas l'envoi.
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
