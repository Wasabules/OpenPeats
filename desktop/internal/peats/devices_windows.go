//go:build windows

package peats

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Paired devices on Windows: the Bluetooth API in bthprops.cpl, which reads
// the system's own device list. No inquiry is issued, so listing is instant
// and never pages the earbuds.

var (
	bthprops            = windows.NewLazySystemDLL("bthprops.cpl")
	procFindFirstDevice = bthprops.NewProc("BluetoothFindFirstDevice")
	procFindNextDevice  = bthprops.NewProc("BluetoothFindNextDevice")
	procFindDeviceClose = bthprops.NewProc("BluetoothFindDeviceClose")
)

func init() { listPaired = listPairedWindows }

// BLUETOOTH_DEVICE_SEARCH_PARAMS (bluetoothapis.h).
//
// Unlike SOCKADDR_BTH (see transport_windows.go), the Bluetooth API structs
// are not packed: natural alignment, as Go lays them out, which puts Radio at
// offset 32 for 40 bytes in all. The API checks dwSize, so a wrong layout
// fails with ERROR_REVISION_MISMATCH instead of misreading; a test pins both
// sizes.
type bthSearchParams struct {
	Size                uint32
	ReturnAuthenticated int32
	ReturnRemembered    int32
	ReturnUnknown       int32
	ReturnConnected     int32
	IssueInquiry        int32
	TimeoutMultiplier   uint8
	Radio               windows.Handle
}

// BLUETOOTH_DEVICE_INFO: 560 bytes, Address at offset 8.
type bthDeviceInfo struct {
	Size          uint32
	Address       uint64
	ClassOfDevice uint32
	Connected     int32
	Remembered    int32
	Authenticated int32
	LastSeen      windows.Systemtime
	LastUsed      windows.Systemtime
	Name          [248]uint16
}

func listPairedWindows() ([]pairedDevice, error) {
	if err := procFindFirstDevice.Find(); err != nil {
		return nil, fmt.Errorf("bthprops.cpl: %w", err)
	}

	// Remembered and authenticated devices make up the system's paired list.
	// Every device returned carries its link state, connected or not.
	params := bthSearchParams{
		ReturnAuthenticated: 1,
		ReturnRemembered:    1,
	}
	params.Size = uint32(unsafe.Sizeof(params))
	var info bthDeviceInfo
	info.Size = uint32(unsafe.Sizeof(info))

	find, _, callErr := procFindFirstDevice.Call(
		uintptr(unsafe.Pointer(&params)), uintptr(unsafe.Pointer(&info)))
	if find == 0 {
		// No device at all is not a failure: it is an empty list.
		if errors.Is(callErr, windows.ERROR_NO_MORE_ITEMS) {
			return nil, nil
		}
		return nil, fmt.Errorf("BluetoothFindFirstDevice: %w", callErr)
	}
	defer procFindDeviceClose.Call(find)

	var out []pairedDevice
	for {
		if info.Remembered != 0 || info.Authenticated != 0 {
			out = append(out, pairedDevice{
				Device: Device{
					Name:      windows.UTF16ToString(info.Name[:]),
					Address:   FormatMAC(fromBTHAddr(info.Address)),
					Connected: info.Connected != 0,
				},
				class: info.ClassOfDevice,
			})
		}

		ok, _, callErr := procFindNextDevice.Call(find, uintptr(unsafe.Pointer(&info)))
		if ok == 0 {
			if errors.Is(callErr, windows.ERROR_NO_MORE_ITEMS) {
				return out, nil
			}
			return out, fmt.Errorf("BluetoothFindNextDevice: %w", callErr)
		}
	}
}

// toBTHAddr packs an address into a BTH_ADDR: the 48 bits in the low bytes of
// a 64-bit integer, read in the natural MAC order.
func toBTHAddr(addr [6]byte) uint64 {
	var bt uint64
	for _, b := range addr {
		bt = bt<<8 | uint64(b)
	}
	return bt
}

// fromBTHAddr is the inverse of toBTHAddr.
func fromBTHAddr(bt uint64) [6]byte {
	var addr [6]byte
	for i := range addr {
		addr[i] = byte(bt >> (8 * (5 - i)))
	}
	return addr
}
