//go:build linux

package peats

import (
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestPairedFromBlueZ(t *testing.T) {
	objects := blueZObjects{
		"/org/bluez/hci0": {
			"org.bluez.Adapter1": {"Address": dbus.MakeVariant("00:1A:7D:DA:71:13")},
		},
		"/org/bluez/hci0/dev_98_80_BB_40_D4_ED": {
			"org.bluez.Device1": {
				"Address":   dbus.MakeVariant("98:80:BB:40:D4:ED"),
				"Alias":     dbus.MakeVariant("My earbuds"),
				"Name":      dbus.MakeVariant("SOUNDPEATS H3"),
				"Paired":    dbus.MakeVariant(true),
				"Connected": dbus.MakeVariant(true),
				"Class":     dbus.MakeVariant(uint32(0x240404)),
			},
		},
		// Seen in a scan, never paired.
		"/org/bluez/hci0/dev_11_22_33_44_55_66": {
			"org.bluez.Device1": {
				"Address": dbus.MakeVariant("11:22:33:44:55:66"),
				"Paired":  dbus.MakeVariant(false),
				"Class":   dbus.MakeVariant(uint32(0x240404)),
			},
		},
		// Paired, with no alias and a lower-case address.
		"/org/bluez/hci0/dev_AA_BB_CC_DD_EE_FF": {
			"org.bluez.Device1": {
				"Address": dbus.MakeVariant("aa:bb:cc:dd:ee:ff"),
				"Name":    dbus.MakeVariant("Speaker"),
				"Paired":  dbus.MakeVariant(true),
			},
		},
	}

	got := map[string]pairedDevice{}
	for _, d := range pairedFromBlueZ(objects) {
		got[d.Address] = d
	}
	if len(got) != 2 {
		t.Fatalf("got %d devices, want the 2 paired ones: %v", len(got), got)
	}
	want := pairedDevice{
		Device: Device{Name: "My earbuds", Address: "98:80:BB:40:D4:ED", Connected: true},
		class:  0x240404,
	}
	if d := got["98:80:BB:40:D4:ED"]; d != want {
		t.Errorf("earbuds = %+v, want %+v", d, want)
	}
	if d := got["AA:BB:CC:DD:EE:FF"]; d.Name != "Speaker" || d.Connected || d.class != 0 {
		t.Errorf("speaker = %+v, want its Name, disconnected, no class", d)
	}
}
