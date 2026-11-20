package freedesktop

import (
	"slices"
	"strings"
	"sync"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
)

// The menu of the panel icon, over com.canonical.dbusmenu: the host draws it
// and reports clicks back. The protocol was written for nested menus; this one
// is flat, so item n has id n and the root is 0.

const menuInterface = "com.canonical.dbusmenu"

// MenuItem is one entry of the menu.
type MenuItem struct {
	Label     string
	Separator bool
	Disabled  bool
	// Radio and Check give the item an indicator; Checked is its state.
	Radio   bool
	Check   bool
	Checked bool
	// OnClick runs on its own goroutine: it may block.
	OnClick func()
}

// properties returns the item as dbusmenu describes it. Defaults are left
// out, as the specification asks: a host fills them in.
func (it MenuItem) properties() map[string]dbus.Variant {
	if it.Separator {
		return map[string]dbus.Variant{"type": dbus.MakeVariant("separator")}
	}
	props := map[string]dbus.Variant{
		"label": dbus.MakeVariant(escapeMnemonics(it.Label)),
	}
	if it.Disabled {
		props["enabled"] = dbus.MakeVariant(false)
	}
	switch {
	case it.Radio:
		props["toggle-type"] = dbus.MakeVariant("radio")
	case it.Check:
		props["toggle-type"] = dbus.MakeVariant("checkmark")
	}
	if it.Radio || it.Check {
		state := int32(0)
		if it.Checked {
			state = 1
		}
		props["toggle-state"] = dbus.MakeVariant(state)
	}
	return props
}

// escapeMnemonics doubles underscores: dbusmenu reads a single one as the
// mnemonic marker, and a device name may well contain one.
func escapeMnemonics(label string) string {
	return strings.ReplaceAll(label, "_", "__")
}

// menuLayout is the (ia{sv}av) node GetLayout returns: the children are
// variants holding nodes of the same shape.
type menuLayout struct {
	ID         int32
	Properties map[string]dbus.Variant
	Children   []dbus.Variant
}

// menuItemProperties is one (ia{sv}) entry of GetGroupProperties and of
// ItemsPropertiesUpdated.
type menuItemProperties struct {
	ID         int32
	Properties map[string]dbus.Variant
}

// menuRemovedProperties is one (ias) entry of ItemsPropertiesUpdated.
type menuRemovedProperties struct {
	ID    int32
	Names []string
}

// menuEvent is one (isvu) entry of EventGroup.
type menuEvent struct {
	ID        int32
	EventID   string
	Data      dbus.Variant
	Timestamp uint32
}

type menu struct {
	conn *dbus.Conn
	path dbus.ObjectPath

	mu       sync.Mutex
	revision uint32
	items    []MenuItem
}

const menuIntrospection = `<node>
 <interface name="com.canonical.dbusmenu">
  <property name="Version" type="u" access="read"/>
  <property name="TextDirection" type="s" access="read"/>
  <property name="Status" type="s" access="read"/>
  <property name="IconThemePath" type="as" access="read"/>
  <method name="GetLayout">
   <arg type="i" name="parentId" direction="in"/>
   <arg type="i" name="recursionDepth" direction="in"/>
   <arg type="as" name="propertyNames" direction="in"/>
   <arg type="u" name="revision" direction="out"/>
   <arg type="(ia{sv}av)" name="layout" direction="out"/>
  </method>
  <method name="GetGroupProperties">
   <arg type="ai" name="ids" direction="in"/>
   <arg type="as" name="propertyNames" direction="in"/>
   <arg type="a(ia{sv})" name="properties" direction="out"/>
  </method>
  <method name="GetProperty">
   <arg type="i" name="id" direction="in"/>
   <arg type="s" name="name" direction="in"/>
   <arg type="v" name="value" direction="out"/>
  </method>
  <method name="Event">
   <arg type="i" name="id" direction="in"/>
   <arg type="s" name="eventId" direction="in"/>
   <arg type="v" name="data" direction="in"/>
   <arg type="u" name="timestamp" direction="in"/>
  </method>
  <method name="EventGroup">
   <arg type="a(isvu)" name="events" direction="in"/>
   <arg type="ai" name="idErrors" direction="out"/>
  </method>
  <method name="AboutToShow">
   <arg type="i" name="id" direction="in"/>
   <arg type="b" name="needUpdate" direction="out"/>
  </method>
  <method name="AboutToShowGroup">
   <arg type="ai" name="ids" direction="in"/>
   <arg type="ai" name="updatesNeeded" direction="out"/>
   <arg type="ai" name="idErrors" direction="out"/>
  </method>
  <signal name="ItemsPropertiesUpdated">
   <arg type="a(ia{sv})" name="updatedProps"/>
   <arg type="a(ias)" name="removedProps"/>
  </signal>
  <signal name="LayoutUpdated">
   <arg type="u" name="revision"/>
   <arg type="i" name="parent"/>
  </signal>
  <signal name="ItemActivationRequested">
   <arg type="i" name="id"/>
   <arg type="u" name="timestamp"/>
  </signal>
 </interface>` + introspect.IntrospectDataString + propertiesIntrospection + `</node>`

func exportMenu(conn *dbus.Conn, path dbus.ObjectPath) (*menu, error) {
	m := &menu{conn: conn, path: path, revision: 1}
	err := conn.ExportMethodTable(map[string]any{
		"GetLayout":          m.getLayout,
		"GetGroupProperties": m.getGroupProperties,
		"GetProperty":        m.getProperty,
		"Event":              m.event,
		"EventGroup":         m.eventGroup,
		"AboutToShow":        m.aboutToShow,
		"AboutToShowGroup":   m.aboutToShowGroup,
	}, path, menuInterface)
	if err != nil {
		return nil, err
	}
	_, err = exportProperties(conn, path, map[string]map[string]any{
		menuInterface: {
			"Version":       uint32(3),
			"TextDirection": "ltr",
			"Status":        "normal",
			"IconThemePath": []string{},
		},
	})
	if err != nil {
		return nil, err
	}
	if err := conn.Export(introspect.Introspectable(menuIntrospection), path,
		"org.freedesktop.DBus.Introspectable"); err != nil {
		return nil, err
	}
	return m, nil
}

// set replaces the items. When only their properties changed — a label, a
// check mark — the host is told which; when the shape changed it is told to
// fetch the whole layout again. The first form leaves an open menu where it
// is, the second may close it.
func (m *menu) set(items []MenuItem) {
	m.mu.Lock()
	old := m.items
	m.items = slices.Clone(items)
	reshaped := !sameShape(old, items)
	if reshaped {
		m.revision++
	}
	revision := m.revision
	m.mu.Unlock()

	if reshaped {
		_ = m.conn.Emit(m.path, menuInterface+".LayoutUpdated", revision, int32(0))
		return
	}
	updated, removed := propertyChanges(old, items)
	if len(updated) == 0 && len(removed) == 0 {
		return
	}
	_ = m.conn.Emit(m.path, menuInterface+".ItemsPropertiesUpdated", updated, removed)
}

// sameShape reports whether two menus have the same entries in the same
// places, so that only their properties can have changed.
func sameShape(a, b []MenuItem) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Separator != b[i].Separator {
			return false
		}
	}
	return true
}

// propertyChanges lists, for menus of the same shape, the properties that
// changed and those that went away.
func propertyChanges(old, cur []MenuItem) ([]menuItemProperties, []menuRemovedProperties) {
	updated := []menuItemProperties{}
	removed := []menuRemovedProperties{}
	for i := range cur {
		before, after := old[i].properties(), cur[i].properties()
		changed := map[string]dbus.Variant{}
		for name, v := range after {
			if prev, ok := before[name]; !ok || !sameVariant(prev, v) {
				changed[name] = v
			}
		}
		var gone []string
		for name := range before {
			if _, ok := after[name]; !ok {
				gone = append(gone, name)
			}
		}
		id := int32(i + 1)
		if len(changed) > 0 {
			updated = append(updated, menuItemProperties{ID: id, Properties: changed})
		}
		if len(gone) > 0 {
			slices.Sort(gone)
			removed = append(removed, menuRemovedProperties{ID: id, Names: gone})
		}
	}
	return updated, removed
}

func filterProperties(props map[string]dbus.Variant, names []string) map[string]dbus.Variant {
	if len(names) == 0 {
		return props
	}
	out := map[string]dbus.Variant{}
	for _, name := range names {
		if v, ok := props[name]; ok {
			out[name] = v
		}
	}
	return out
}

// item returns the item with the given id; the root has none.
func (m *menu) item(id int32) (MenuItem, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if id < 1 || int(id) > len(m.items) {
		return MenuItem{}, false
	}
	return m.items[id-1], true
}

func rootProperties() map[string]dbus.Variant {
	return map[string]dbus.Variant{"children-display": dbus.MakeVariant("submenu")}
}

func (m *menu) getLayout(parentID, depth int32, names []string) (uint32, menuLayout, *dbus.Error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if parentID != 0 {
		if parentID < 1 || int(parentID) > len(m.items) {
			return 0, menuLayout{}, unknownMenuItem(parentID)
		}
		it := m.items[parentID-1]
		return m.revision, menuLayout{
			ID:         parentID,
			Properties: filterProperties(it.properties(), names),
			Children:   []dbus.Variant{},
		}, nil
	}

	root := menuLayout{
		Properties: filterProperties(rootProperties(), names),
		Children:   []dbus.Variant{},
	}
	if depth != 0 { // -1 is "everything", and the items have no children
		for i, it := range m.items {
			root.Children = append(root.Children, dbus.MakeVariant(menuLayout{
				ID:         int32(i + 1),
				Properties: filterProperties(it.properties(), names),
				Children:   []dbus.Variant{},
			}))
		}
	}
	return m.revision, root, nil
}

func (m *menu) getGroupProperties(ids []int32, names []string) ([]menuItemProperties, *dbus.Error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(ids) == 0 { // an empty list asks for every item
		for i := range m.items {
			ids = append(ids, int32(i+1))
		}
	}
	out := []menuItemProperties{}
	for _, id := range ids {
		var props map[string]dbus.Variant
		switch {
		case id == 0:
			props = rootProperties()
		case id >= 1 && int(id) <= len(m.items):
			props = m.items[id-1].properties()
		default:
			continue // the specification ignores unknown ids here
		}
		out = append(out, menuItemProperties{ID: id, Properties: filterProperties(props, names)})
	}
	return out, nil
}

func (m *menu) getProperty(id int32, name string) (dbus.Variant, *dbus.Error) {
	var props map[string]dbus.Variant
	if id == 0 {
		props = rootProperties()
	} else {
		it, ok := m.item(id)
		if !ok {
			return dbus.Variant{}, unknownMenuItem(id)
		}
		props = it.properties()
	}
	v, ok := props[name]
	if !ok {
		return dbus.Variant{}, dbus.NewError("org.freedesktop.DBus.Error.InvalidArgs",
			[]any{"no property " + name})
	}
	return v, nil
}

func (m *menu) event(id int32, eventID string, _ dbus.Variant, _ uint32) *dbus.Error {
	if id == 0 {
		return nil
	}
	it, ok := m.item(id)
	if !ok {
		return unknownMenuItem(id)
	}
	if eventID == "clicked" && it.OnClick != nil && !it.Disabled && !it.Separator {
		go it.OnClick()
	}
	return nil
}

func (m *menu) eventGroup(events []menuEvent) ([]int32, *dbus.Error) {
	errs := []int32{}
	for _, e := range events {
		if err := m.event(e.ID, e.EventID, e.Data, e.Timestamp); err != nil {
			errs = append(errs, e.ID)
		}
	}
	if len(events) > 0 && len(errs) == len(events) {
		return errs, unknownMenuItem(events[0].ID)
	}
	return errs, nil
}

// aboutToShow never asks for an update: the items are always current.
func (m *menu) aboutToShow(int32) (bool, *dbus.Error) { return false, nil }

func (m *menu) aboutToShowGroup(ids []int32) ([]int32, []int32, *dbus.Error) {
	return []int32{}, []int32{}, nil
}

func unknownMenuItem(id int32) *dbus.Error {
	return dbus.NewError("org.freedesktop.DBus.Error.InvalidArgs",
		[]any{"unknown menu item", id})
}

// menuSnapshot is the menu's state for tests.
func (m *menu) snapshot() (uint32, []MenuItem) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.revision, slices.Clone(m.items)
}
