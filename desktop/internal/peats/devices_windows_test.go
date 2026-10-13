//go:build windows

package peats

import (
	"testing"
	"unsafe"
)

// The Bluetooth API rejects a struct whose dwSize it does not expect. These
// are the sizes of the 64-bit C layouts.
func TestBluetoothAPIStructSizes(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("sizes pinned for 64-bit Windows only")
	}
	if got := unsafe.Sizeof(bthSearchParams{}); got != 40 {
		t.Errorf("BLUETOOTH_DEVICE_SEARCH_PARAMS: %d bytes, want 40", got)
	}
	if got := unsafe.Sizeof(bthDeviceInfo{}); got != 560 {
		t.Errorf("BLUETOOTH_DEVICE_INFO: %d bytes, want 560", got)
	}
	if got := unsafe.Offsetof(bthDeviceInfo{}.Address); got != 8 {
		t.Errorf("BLUETOOTH_DEVICE_INFO.Address at offset %d, want 8", got)
	}
}

func TestBTHAddrRoundTrip(t *testing.T) {
	addr := [6]byte{0x98, 0x80, 0xBB, 0x40, 0xD4, 0xED}
	bt := toBTHAddr(addr)
	if bt != 0x9880BB40D4ED {
		t.Errorf("toBTHAddr = %#x, want 0x9880bb40d4ed", bt)
	}
	if got := fromBTHAddr(bt); got != addr {
		t.Errorf("fromBTHAddr = % X, want % X", got, addr)
	}
}
