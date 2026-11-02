package com.openpeats

import android.annotation.SuppressLint
import android.app.Application
import android.bluetooth.BluetoothAdapter
import android.bluetooth.BluetoothManager
import android.content.Context
import androidx.lifecycle.AndroidViewModel
import com.openpeats.protocol.EqBand
import com.openpeats.service.EarbudsService
import com.openpeats.session.DeviceState
import com.openpeats.session.KnownEarbuds
import com.openpeats.session.PairedDevice
import com.openpeats.ui.EqEditor
import kotlinx.coroutines.flow.StateFlow

/**
 * The screen's window on the session. The link itself outlives this: leaving
 * the app does not close it, only "Close" does.
 */
class DeviceViewModel(app: Application) : AndroidViewModel(app) {

    private val earbuds = (app as OpenPeatsApp).earbuds

    val state: StateFlow<DeviceState> = earbuds.state
    val log: StateFlow<List<String>> = earbuds.log
    val known: StateFlow<KnownEarbuds?> = earbuds.known
    val wanted: StateFlow<String?> = earbuds.wanted
    val keepConnected: StateFlow<Boolean> = earbuds.keepConnected
    val batteryAlerts: StateFlow<Boolean> = earbuds.batteryAlerts
    val eqEditor = EqEditor()

    private fun adapter(): BluetoothAdapter? =
        (getApplication<Application>()
            .getSystemService(Context.BLUETOOTH_SERVICE) as? BluetoothManager)?.adapter

    /** Whether the Bluetooth adapter exists and is switched on. */
    fun bluetoothEnabled(): Boolean = adapter()?.isEnabled == true

    /**
     * Paired earbuds, the last ones used first. Deliberately not filtered by
     * name: other SoundPEATS models share this protocol, and missing features
     * are detected on connection anyway.
     */
    @SuppressLint("MissingPermission")
    fun pairedDevices(): List<PairedDevice> =
        adapter()?.bondedDevices.orEmpty()
            .filter { it.bluetoothClass?.majorDeviceClass == 0x400 } // AUDIO_VIDEO
            .map { PairedDevice(it.name ?: it.address, it.address) }
            .sortedBy { it.address != earbuds.known.value?.address }

    fun connect(address: String) = earbuds.connect(address)
    fun disconnect() = earbuds.stop()

    /**
     * Picks up a link that was kept, when the app opens or gets the permission
     * back. The service could not run without that permission, so it is started
     * again here rather than left to the next change of state.
     */
    fun resume() {
        earbuds.resume()
        if (earbuds.wanted.value != null) EarbudsService.tryStart(getApplication())
    }

    fun setKeepConnected(on: Boolean) = earbuds.setKeepConnected(on)
    fun setBatteryAlerts(on: Boolean) = earbuds.setBatteryAlerts(on)
    fun setScreenOpen(open: Boolean) = earbuds.setScreenOpen(open)

    fun refresh() = earbuds.refresh()
    fun setAncMode(mode: Int) = earbuds.requestAncMode(mode)
    fun setAncLevel(code: Int) = earbuds.setAncLevel(code)
    fun setTransparencyLevel(code: Int) = earbuds.setTransparencyLevel(code)
    fun setGameMode(on: Boolean) = earbuds.setGameMode(on)
    fun setLeAudio(on: Boolean) = earbuds.setLeAudio(on)
    fun setTouchDisabled(on: Boolean) = earbuds.setTouchDisabled(on)
    fun setVoicePack(code: Int) = earbuds.setVoicePack(code)
    fun setPromptVolume(v: Int) = earbuds.setPromptVolume(v)
    fun setMultipoint(on: Boolean) = earbuds.setMultipoint(on)
    fun setLdac(on: Boolean) = earbuds.setLdac(on)
    fun applyEq(bands: List<EqBand>, masterGain: Double) = earbuds.applyEq(bands, masterGain)
}
