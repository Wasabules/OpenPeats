//go:build !linux && !windows

package peats

import "time"

// Platforms with no RFCOMM transport implemented.
//
// macOS — feasible, just not done yet. RFCOMM there goes through the
// IOBluetooth framework (Objective-C, asynchronous delegate-based API): it
// needs a cgo bridge that opens an IOBluetoothRFCOMMChannel and turns the
// delegate callbacks into blocking reads. See IOBluetoothDevice
// -performSDPQuery: then -openRFCOMMChannelSync:withChannelID:delegate:.
// Unlike Linux and Windows there is no socket API, so the code cannot be
// written as a mirror of the other two transports.
//
// iOS — out of reach. SPP there is reserved for MFi-certified accessories,
// through the ExternalAccessory framework. These earbuds are not certified.
// No application-side workaround exists.
//
// Android — the kernel is Linux and does support AF_BLUETOOTH, but SELinux
// and the permission model block raw Bluetooth sockets from the NDK. The
// supported path is android.bluetooth.BluetoothSocket, on the Kotlin side.

func init() {
	dialRFCOMM = func([6]byte, uint8, time.Duration) (rfcommConn, error) {
		return nil, ErrUnsupported
	}
}

func isTimeout(error) bool { return false }
