// Package freedesktop puts OpenPeats into a Linux desktop over D-Bus: an icon
// in the panel (StatusNotifierItem and its menu), the control service the
// GNOME Shell extension and the launcher's actions go through, and the battery
// level handed to BlueZ for the system's own menus.
//
// Everything here is plain D-Bus, written against the specifications rather
// than a toolkit: the window belongs to Wails and WebKitGTK, and none of these
// surfaces needs GTK. The package knows nothing about the earbuds either — the
// application decides what to show and what a click does.
package freedesktop

import (
	"maps"
	"reflect"
	"slices"
	"sync"

	"github.com/godbus/dbus/v5"
)

const propertiesInterface = "org.freedesktop.DBus.Properties"

// propertiesIntrospection describes org.freedesktop.DBus.Properties, for the
// introspection data of every object that has properties.
const propertiesIntrospection = `
 <interface name="org.freedesktop.DBus.Properties">
  <method name="Get">
   <arg name="interface" direction="in" type="s"/>
   <arg name="property" direction="in" type="s"/>
   <arg name="value" direction="out" type="v"/>
  </method>
  <method name="GetAll">
   <arg name="interface" direction="in" type="s"/>
   <arg name="props" direction="out" type="a{sv}"/>
  </method>
  <method name="Set">
   <arg name="interface" direction="in" type="s"/>
   <arg name="property" direction="in" type="s"/>
   <arg name="value" direction="in" type="v"/>
  </method>
  <signal name="PropertiesChanged">
   <arg name="interface" type="s"/>
   <arg name="changed_properties" type="a{sv}"/>
   <arg name="invalidated_properties" type="as"/>
  </signal>
 </interface>
`

// properties implements org.freedesktop.DBus.Properties for read-only values.
//
// Unlike godbus's prop package, an update only emits what actually changed,
// and emits it as one PropertiesChanged signal: a client redraws once per
// state change instead of once per property, and not at all for a refresh
// that changed nothing.
type properties struct {
	conn *dbus.Conn
	path dbus.ObjectPath

	mu     sync.Mutex
	values map[string]map[string]dbus.Variant
}

func exportProperties(conn *dbus.Conn, path dbus.ObjectPath, initial map[string]map[string]any) (*properties, error) {
	p := &properties{
		conn:   conn,
		path:   path,
		values: map[string]map[string]dbus.Variant{},
	}
	for iface, props := range initial {
		p.values[iface] = map[string]dbus.Variant{}
		for name, v := range props {
			p.values[iface][name] = dbus.MakeVariant(v)
		}
	}
	err := conn.ExportMethodTable(map[string]any{
		"Get":    p.get,
		"GetAll": p.getAll,
		"Set":    p.set,
	}, path, propertiesInterface)
	if err != nil {
		return nil, err
	}
	return p, nil
}

func (p *properties) get(iface, name string) (dbus.Variant, *dbus.Error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	props, ok := p.values[iface]
	if !ok {
		return dbus.Variant{}, unknownInterface(iface)
	}
	v, ok := props[name]
	if !ok {
		return dbus.Variant{}, dbus.NewError("org.freedesktop.DBus.Error.UnknownProperty",
			[]any{"no property " + name + " on " + iface})
	}
	return v, nil
}

func (p *properties) getAll(iface string) (map[string]dbus.Variant, *dbus.Error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	props, ok := p.values[iface]
	if !ok {
		return nil, unknownInterface(iface)
	}
	return maps.Clone(props), nil
}

func (p *properties) set(iface, name string, _ dbus.Variant) *dbus.Error {
	return dbus.NewError("org.freedesktop.DBus.Error.PropertyReadOnly",
		[]any{name + " on " + iface + " is read-only"})
}

// value returns the current value of a property, nil when it does not exist.
func (p *properties) value(iface, name string) any {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.values[iface][name].Value()
}

// all returns a copy of every property of one interface.
func (p *properties) all(iface string) map[string]dbus.Variant {
	p.mu.Lock()
	defer p.mu.Unlock()
	return maps.Clone(p.values[iface])
}

// update stores new values and emits PropertiesChanged for those that
// differ. It returns the names that changed, sorted.
func (p *properties) update(iface string, values map[string]any) []string {
	changed := map[string]dbus.Variant{}

	p.mu.Lock()
	props := p.values[iface]
	if props == nil {
		props = map[string]dbus.Variant{}
		p.values[iface] = props
	}
	for name, v := range values {
		nv := dbus.MakeVariant(v)
		if old, ok := props[name]; ok && sameVariant(old, nv) {
			continue
		}
		props[name] = nv
		changed[name] = nv
	}
	p.mu.Unlock()

	if len(changed) == 0 {
		return nil
	}
	// A failed emit means the bus went away: the next update tries again,
	// and there is nobody to tell in the meantime.
	_ = p.conn.Emit(p.path, propertiesInterface+".PropertiesChanged",
		iface, changed, []string{})
	return slices.Sorted(maps.Keys(changed))
}

func sameVariant(a, b dbus.Variant) bool {
	return a.Signature() == b.Signature() && reflect.DeepEqual(a.Value(), b.Value())
}

func unknownInterface(iface string) *dbus.Error {
	return dbus.NewError("org.freedesktop.DBus.Error.UnknownInterface",
		[]any{"no interface " + iface})
}
