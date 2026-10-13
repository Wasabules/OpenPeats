package peats

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
)

// Device is a Bluetooth device paired with this computer, as the system lists
// it. Listing asks the operating system, never the earbuds: it needs no
// connection and sends nothing over the air.
type Device struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	// Connected reports whether the system holds a link to the device right
	// now. The control channel only opens on a connected device: paired is
	// not enough, which is what an absent pair in its case looks like.
	Connected bool `json:"connected"`
}

// pairedDevice is a Device plus its Bluetooth Class of Device, which only the
// filtering below needs.
type pairedDevice struct {
	Device
	class uint32
}

// listPaired returns every device paired with this computer. Implemented once
// per platform, in its own file, like dialRFCOMM; left nil where there is
// none.
var listPaired func() ([]pairedDevice, error)

// PairedAudioDevices lists the audio devices paired with this computer,
// connected ones first.
//
// Deliberately not filtered by name, like the Android picker: other SoundPEATS
// models share this protocol, a pair can be renamed, and features a model
// lacks are detected once connected anyway.
func PairedAudioDevices() ([]Device, error) {
	if listPaired == nil {
		return nil, ErrUnsupported
	}
	all, err := listPaired()
	if err != nil {
		return nil, err
	}
	out := []Device{}
	for _, d := range all {
		if isAudioVideo(d.class) {
			out = append(out, d.Device)
		}
	}
	sortDevices(out)
	return out, nil
}

// isAudioVideo reads the major device class out of a Bluetooth Class of
// Device: bits 8 to 12, where 0x04 is Audio/Video — headsets, earbuds,
// speakers. Android's BluetoothClass.Device.Major.AUDIO_VIDEO (0x400) is the
// same field, left unshifted.
func isAudioVideo(class uint32) bool { return (class>>8)&0x1f == 0x04 }

// sortDevices puts connected devices first — the earbuds out of their case are
// the ones that can answer — then orders by name.
func sortDevices(ds []Device) {
	slices.SortStableFunc(ds, func(a, b Device) int {
		if a.Connected != b.Connected {
			if a.Connected {
				return -1
			}
			return 1
		}
		return cmp.Or(
			cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)),
			cmp.Compare(a.Address, b.Address),
		)
	})
}

// FormatMAC is the inverse of ParseMAC: most significant byte first, upper
// case, colon separated.
func FormatMAC(addr [6]byte) string {
	return fmt.Sprintf("%02X:%02X:%02X:%02X:%02X:%02X",
		addr[0], addr[1], addr[2], addr[3], addr[4], addr[5])
}
