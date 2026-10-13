//go:build linux

package peats

import (
	"fmt"

	"github.com/godbus/dbus/v5"
)

// Paired devices on Linux: BlueZ, over the system D-Bus. It is the only
// place the paired list can be read from — /var/lib/bluetooth belongs to
// root — so unlike the RFCOMM transport this does need the bluetooth daemon,
// which any desktop able to pair devices already runs.

func init() { listPaired = listPairedBlueZ }

// blueZObjects is the reply of ObjectManager.GetManagedObjects: object path,
// then interface name, then property name.
type blueZObjects = map[dbus.ObjectPath]map[string]map[string]dbus.Variant

func listPairedBlueZ() ([]pairedDevice, error) {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, fmt.Errorf("system D-Bus: %w", err)
	}
	defer conn.Close()

	var objects blueZObjects
	err = conn.Object("org.bluez", "/").
		Call("org.freedesktop.DBus.ObjectManager.GetManagedObjects", 0).
		Store(&objects)
	if err != nil {
		return nil, fmt.Errorf("BlueZ: %w", err)
	}
	return pairedFromBlueZ(objects), nil
}

// pairedFromBlueZ keeps the paired org.bluez.Device1 objects. A property of
// the wrong type, or missing, reads as its zero value: a device BlueZ knows
// too little about is skipped rather than failing the whole list.
func pairedFromBlueZ(objects blueZObjects) []pairedDevice {
	var out []pairedDevice
	for _, ifaces := range objects {
		props, ok := ifaces["org.bluez.Device1"]
		if !ok || !variant[bool](props, "Paired") {
			continue
		}
		addr := variant[string](props, "Address")
		mac, err := ParseMAC(addr)
		if err != nil {
			continue
		}
		// Alias is the name the user gave the device, defaulting to the
		// one it advertises.
		name := variant[string](props, "Alias")
		if name == "" {
			name = variant[string](props, "Name")
		}
		out = append(out, pairedDevice{
			Device: Device{
				Name:      name,
				Address:   FormatMAC(mac),
				Connected: variant[bool](props, "Connected"),
			},
			class: variant[uint32](props, "Class"),
		})
	}
	return out
}

func variant[T any](props map[string]dbus.Variant, name string) T {
	v, _ := props[name].Value().(T)
	return v
}
