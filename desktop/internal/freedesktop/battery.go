package freedesktop

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

// The battery level, handed to BlueZ through its battery provider API. BlueZ
// publishes it as the device's org.bluez.Battery1, UPower picks that up, and
// GNOME shows it in Settings > Bluetooth and in the system menu — with no
// extension involved.
//
// Only for earbuds that do not report a level themselves. Many do, over the
// HFP audio link (the H3 does): BlueZ then has one already, live, and keeps a
// single source per device, so OpenPeats stays out of the way.
//
// BlueZ keeps one level per device, so the earbuds are summed up by the
// weaker of the two: that is the one that will run out first.

const (
	batteryProviderInterface = "org.bluez.BatteryProvider1"
	batteryManagerInterface  = "org.bluez.BatteryProviderManager1"
	objectManagerInterface   = "org.freedesktop.DBus.ObjectManager"
	batteryRoot              = dbus.ObjectPath("/io/github/wasabules/OpenPeats/battery")
)

// managedObjects is the reply of ObjectManager.GetManagedObjects.
type managedObjects = map[dbus.ObjectPath]map[string]map[string]dbus.Variant

// BatteryProvider publishes one device's battery level through BlueZ.
type BatteryProvider struct {
	conn *dbus.Conn

	// wanted is the latest request; the worker applies it. A burst of
	// updates collapses into the last one.
	wantMu sync.Mutex
	wanted batteryRequest
	wake   chan struct{}
	done   chan struct{}
	once   sync.Once

	// Owned by the worker, and read under objMu by GetManagedObjects,
	// which BlueZ calls from its own goroutine.
	objMu      sync.Mutex
	registered map[dbus.ObjectPath]bool // adapters we registered with
	current    *batteryObject
}

type batteryRequest struct {
	mac     string
	percent int // <0: no level, remove it
}

type batteryObject struct {
	path    dbus.ObjectPath
	mac     string
	device  dbus.ObjectPath
	percent byte
	props   *properties
}

// NewBatteryProvider connects to the system bus and exports the provider's
// root. Nothing reaches BlueZ until the first level.
func NewBatteryProvider() (*BatteryProvider, error) {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, err
	}
	b := &BatteryProvider{
		conn:       conn,
		wanted:     batteryRequest{percent: -1},
		wake:       make(chan struct{}, 1),
		done:       make(chan struct{}),
		registered: map[dbus.ObjectPath]bool{},
	}
	err = conn.ExportMethodTable(map[string]any{
		"GetManagedObjects": b.getManagedObjects,
	}, batteryRoot, objectManagerInterface)
	if err != nil {
		conn.Close()
		return nil, err
	}
	go b.run()
	return b, nil
}

// Set publishes the level of the device with the given address. A negative
// level, or an empty address, withdraws it: a level kept after the link went
// down would be a stale one.
func (b *BatteryProvider) Set(mac string, percent int) {
	if mac == "" || percent > 100 {
		percent = -1
	}
	b.wantMu.Lock()
	b.wanted = batteryRequest{mac: strings.ToUpper(mac), percent: percent}
	b.wantMu.Unlock()
	select {
	case b.wake <- struct{}{}:
	default: // a wake-up is already pending and will read the latest
	}
}

// Close withdraws the level and drops the connection; BlueZ forgets the
// provider when its owner leaves the bus.
func (b *BatteryProvider) Close() {
	b.once.Do(func() {
		close(b.done)
		b.conn.Close()
	})
}

func (b *BatteryProvider) run() {
	for {
		select {
		case <-b.done:
			return
		case <-b.wake:
			b.wantMu.Lock()
			req := b.wanted
			b.wantMu.Unlock()
			b.apply(req)
		}
	}
}

func (b *BatteryProvider) apply(req batteryRequest) {
	b.objMu.Lock()
	cur := b.current
	b.objMu.Unlock()

	if req.percent < 0 {
		if cur != nil {
			b.remove(cur)
		}
		return
	}

	if cur != nil && cur.mac == req.mac {
		if cur.percent != byte(req.percent) {
			b.objMu.Lock()
			cur.percent = byte(req.percent)
			b.objMu.Unlock()
			cur.props.update(batteryProviderInterface, map[string]any{"Percentage": byte(req.percent)})
		}
		return
	}
	if cur != nil {
		b.remove(cur)
	}

	device, adapter, hasBattery, ok := b.findDevice(req.mac)
	if !ok {
		return // not a device BlueZ knows: nothing to attach a level to
	}
	if hasBattery {
		// The level comes in already, typically over the HFP audio link,
		// and live where this one is polled. BlueZ keeps one source per
		// device: taking the place would only make it worse.
		return
	}
	obj := &batteryObject{
		path:    batteryRoot + "/" + device[strings.LastIndex(string(device), "/")+1:],
		mac:     req.mac,
		device:  device,
		percent: byte(req.percent),
	}
	props, err := exportProperties(b.conn, obj.path, map[string]map[string]any{
		batteryProviderInterface: obj.properties(),
	})
	if err != nil {
		return
	}
	obj.props = props

	b.objMu.Lock()
	b.current = obj
	needRegister := !b.registered[adapter]
	b.objMu.Unlock()

	if needRegister {
		// BlueZ reads the objects with GetManagedObjects as it registers
		// the provider: this one is already there to be found.
		if b.register(adapter) {
			b.objMu.Lock()
			b.registered[adapter] = true
			b.objMu.Unlock()
		}
		return
	}
	_ = b.conn.Emit(batteryRoot, objectManagerInterface+".InterfacesAdded",
		obj.path, map[string]map[string]dbus.Variant{
			batteryProviderInterface: props.all(batteryProviderInterface),
		})
}

func (o *batteryObject) properties() map[string]any {
	return map[string]any{
		"Percentage": o.percent,
		"Source":     "OpenPeats",
		"Device":     o.device,
	}
}

func (b *BatteryProvider) remove(obj *batteryObject) {
	b.objMu.Lock()
	if b.current == obj {
		b.current = nil
	}
	b.objMu.Unlock()
	_ = b.conn.Emit(batteryRoot, objectManagerInterface+".InterfacesRemoved",
		obj.path, []string{batteryProviderInterface})
	_ = b.conn.Export(nil, obj.path, propertiesInterface)
}

func (b *BatteryProvider) register(adapter dbus.ObjectPath) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return b.conn.Object("org.bluez", adapter).CallWithContext(ctx,
		batteryManagerInterface+".RegisterBatteryProvider", 0, batteryRoot).Err == nil
}

func (b *BatteryProvider) getManagedObjects() (managedObjects, *dbus.Error) {
	b.objMu.Lock()
	defer b.objMu.Unlock()
	out := managedObjects{}
	if obj := b.current; obj != nil {
		out[obj.path] = map[string]map[string]dbus.Variant{
			batteryProviderInterface: obj.props.all(batteryProviderInterface),
		}
	}
	return out, nil
}

// findDevice looks the address up among the devices BlueZ knows, and returns
// the device's object, the adapter it hangs off, and whether it has a battery
// level already.
func (b *BatteryProvider) findDevice(mac string) (device, adapter dbus.ObjectPath, hasBattery, ok bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var objects managedObjects
	err := b.conn.Object("org.bluez", "/").CallWithContext(ctx,
		objectManagerInterface+".GetManagedObjects", 0).Store(&objects)
	if err != nil {
		return "", "", false, false
	}
	device, adapter, ok = deviceByAddress(objects, mac)
	_, hasBattery = objects[device]["org.bluez.Battery1"]
	return device, adapter, hasBattery, ok
}

// deviceByAddress finds the org.bluez.Device1 with the given address.
func deviceByAddress(objects managedObjects, mac string) (device, adapter dbus.ObjectPath, ok bool) {
	for path, ifaces := range objects {
		props, isDevice := ifaces["org.bluez.Device1"]
		if !isDevice {
			continue
		}
		addr, _ := props["Address"].Value().(string)
		if !strings.EqualFold(addr, mac) {
			continue
		}
		adapter, _ := props["Adapter"].Value().(dbus.ObjectPath)
		if adapter == "" {
			continue
		}
		return path, adapter, true
	}
	return "", "", false
}
