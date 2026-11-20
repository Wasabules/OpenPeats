package freedesktop

import (
	"context"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
)

// The panel icon is a StatusNotifierItem: KDE's protocol, which GNOME draws
// through the AppIndicator extension Ubuntu enables by default. The item
// registers with a watcher; the watcher's hosts draw it and its menu.

const (
	itemInterface    = "org.kde.StatusNotifierItem"
	itemPath         = dbus.ObjectPath("/StatusNotifierItem")
	menuPath         = dbus.ObjectPath("/StatusNotifierItem/Menu")
	watcherName      = "org.kde.StatusNotifierWatcher"
	watcherPath      = dbus.ObjectPath("/StatusNotifierWatcher")
	watcherInterface = "org.kde.StatusNotifierWatcher"
)

// TrayIcon is what the panel shows.
type TrayIcon struct {
	// IconName is a name from the icon theme.
	IconName string
	// Title names the item, for hosts that list items by name.
	Title string
	// Tooltip is the text under the title in the tooltip.
	Tooltip string
	// Label is short text drawn next to the icon, where the host supports
	// Ayatana's label extension (the GNOME one does). Empty for none.
	Label string
}

// Tray is the panel icon and its menu.
type Tray struct {
	conn  *dbus.Conn
	props *properties
	menu  *menu

	mu         sync.Mutex
	registered bool

	onActivate func()
	signals    chan *dbus.Signal
	done       chan struct{}
	closeOnce  sync.Once
}

// pixmap is one (iiay) icon image; the item sends none, but the type has to
// be right for hosts that read the property anyway.
type pixmap struct {
	Width, Height int32
	Data          []byte
}

// tooltip is the (sa(iiay)ss) ToolTip property.
type tooltip struct {
	IconName string
	Pixmap   []pixmap
	Title    string
	Text     string
}

const itemIntrospection = `<node>
 <interface name="org.kde.StatusNotifierItem">
  <property name="Category" type="s" access="read"/>
  <property name="Id" type="s" access="read"/>
  <property name="Title" type="s" access="read"/>
  <property name="Status" type="s" access="read"/>
  <property name="WindowId" type="i" access="read"/>
  <property name="IconThemePath" type="s" access="read"/>
  <property name="IconName" type="s" access="read"/>
  <property name="IconPixmap" type="a(iiay)" access="read"/>
  <property name="OverlayIconName" type="s" access="read"/>
  <property name="OverlayIconPixmap" type="a(iiay)" access="read"/>
  <property name="AttentionIconName" type="s" access="read"/>
  <property name="AttentionIconPixmap" type="a(iiay)" access="read"/>
  <property name="AttentionMovieName" type="s" access="read"/>
  <property name="ToolTip" type="(sa(iiay)ss)" access="read"/>
  <property name="ItemIsMenu" type="b" access="read"/>
  <property name="Menu" type="o" access="read"/>
  <property name="XAyatanaLabel" type="s" access="read"/>
  <property name="XAyatanaLabelGuide" type="s" access="read"/>
  <property name="XAyatanaOrderingIndex" type="u" access="read"/>
  <method name="ContextMenu"><arg name="x" type="i" direction="in"/><arg name="y" type="i" direction="in"/></method>
  <method name="Activate"><arg name="x" type="i" direction="in"/><arg name="y" type="i" direction="in"/></method>
  <method name="SecondaryActivate"><arg name="x" type="i" direction="in"/><arg name="y" type="i" direction="in"/></method>
  <method name="Scroll"><arg name="delta" type="i" direction="in"/><arg name="orientation" type="s" direction="in"/></method>
  <signal name="NewTitle"/>
  <signal name="NewIcon"/>
  <signal name="NewAttentionIcon"/>
  <signal name="NewOverlayIcon"/>
  <signal name="NewToolTip"/>
  <signal name="NewStatus"><arg name="status" type="s"/></signal>
  <signal name="XAyatanaNewLabel"><arg name="label" type="s"/><arg name="guide" type="s"/></signal>
 </interface>` + introspect.IntrospectDataString + propertiesIntrospection + `</node>`

// NewTray exports the icon and its menu on conn and registers it with the
// watcher, now and whenever a watcher appears later — the shell restarting,
// or the AppIndicator extension being switched on.
//
// onActivate runs when the host activates the item rather than opening its
// menu. GNOME opens the menu on a click, so it mostly serves other hosts.
func NewTray(conn *dbus.Conn, id string, icon TrayIcon, onActivate func()) (*Tray, error) {
	t := &Tray{
		conn:       conn,
		onActivate: onActivate,
		signals:    make(chan *dbus.Signal, 16),
		done:       make(chan struct{}),
	}

	var err error
	if t.menu, err = exportMenu(conn, menuPath); err != nil {
		return nil, err
	}
	t.props, err = exportProperties(conn, itemPath, map[string]map[string]any{
		itemInterface: {
			"Category":              "Hardware",
			"Id":                    id,
			"Title":                 icon.Title,
			"Status":                "Active",
			"WindowId":              int32(0),
			"IconThemePath":         "",
			"IconName":              icon.IconName,
			"IconPixmap":            []pixmap{},
			"OverlayIconName":       "",
			"OverlayIconPixmap":     []pixmap{},
			"AttentionIconName":     "",
			"AttentionIconPixmap":   []pixmap{},
			"AttentionMovieName":    "",
			"ToolTip":               tooltip{Pixmap: []pixmap{}, Title: icon.Title, Text: icon.Tooltip},
			"ItemIsMenu":            true,
			"Menu":                  menuPath,
			"XAyatanaLabel":         icon.Label,
			"XAyatanaLabelGuide":    "",
			"XAyatanaOrderingIndex": uint32(0),
		},
	})
	if err != nil {
		return nil, err
	}
	err = conn.ExportMethodTable(map[string]any{
		"ContextMenu":       func(x, y int32) *dbus.Error { return nil },
		"Activate":          t.activate,
		"SecondaryActivate": func(x, y int32) *dbus.Error { return nil },
		"Scroll":            func(delta int32, orientation string) *dbus.Error { return nil },
	}, itemPath, itemInterface)
	if err != nil {
		return nil, err
	}
	if err := conn.Export(introspect.Introspectable(itemIntrospection), itemPath,
		"org.freedesktop.DBus.Introspectable"); err != nil {
		return nil, err
	}

	// Follow the watcher before registering, so one appearing in between
	// is not missed.
	if err := conn.AddMatchSignal(
		dbus.WithMatchSender("org.freedesktop.DBus"),
		dbus.WithMatchInterface("org.freedesktop.DBus"),
		dbus.WithMatchMember("NameOwnerChanged"),
		dbus.WithMatchArg(0, watcherName),
	); err != nil {
		return nil, err
	}
	conn.Signal(t.signals)
	go t.follow()

	t.register()
	return t, nil
}

func (t *Tray) activate(x, y int32) *dbus.Error {
	if t.onActivate != nil {
		go t.onActivate()
	}
	return nil
}

// follow registers the item again each time a watcher takes the name, and
// forgets the registration when the watcher goes away.
func (t *Tray) follow() {
	for {
		select {
		case <-t.done:
			return
		case sig, ok := <-t.signals:
			if !ok {
				return
			}
			if sig.Name != "org.freedesktop.DBus.NameOwnerChanged" || len(sig.Body) != 3 {
				continue
			}
			if name, _ := sig.Body[0].(string); name != watcherName {
				continue
			}
			if owner, _ := sig.Body[2].(string); owner == "" {
				t.mu.Lock()
				t.registered = false
				t.mu.Unlock()
				continue
			}
			t.register()
		}
	}
}

// register hands the item to the watcher. Passing the object path rather than
// a bus name makes the watcher take the sender as the item's address, which
// both the GNOME and the KDE watchers accept.
func (t *Tray) register() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	call := t.conn.Object(watcherName, watcherPath).CallWithContext(ctx,
		watcherInterface+".RegisterStatusNotifierItem", 0, string(itemPath))
	t.mu.Lock()
	t.registered = call.Err == nil
	t.mu.Unlock()
}

// Visible reports whether a host is drawing the icon right now: the item is
// registered and the watcher has at least one host. Without one, closing the
// window would leave nothing on screen to bring it back.
func (t *Tray) Visible() bool {
	t.mu.Lock()
	registered := t.registered
	t.mu.Unlock()
	return registered && TrayHostAvailable(t.conn)
}

// TrayHostAvailable reports whether a StatusNotifierItem would be drawn: a
// watcher runs and a host has registered with it.
func TrayHostAvailable(conn *dbus.Conn) bool {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var v dbus.Variant
	err := conn.Object(watcherName, watcherPath).CallWithContext(ctx,
		propertiesInterface+".Get", 0, watcherInterface, "IsStatusNotifierHostRegistered").Store(&v)
	if err != nil {
		return false
	}
	ok, _ := v.Value().(bool)
	return ok
}

// Update changes the icon and the menu. Hosts are told about what changed
// only: the legacy New* signals for the older ones, PropertiesChanged for the
// rest.
func (t *Tray) Update(icon TrayIcon, items []MenuItem) {
	changed := t.props.update(itemInterface, map[string]any{
		"Title":         icon.Title,
		"IconName":      icon.IconName,
		"ToolTip":       tooltip{Pixmap: []pixmap{}, Title: icon.Title, Text: icon.Tooltip},
		"XAyatanaLabel": icon.Label,
	})
	for _, name := range changed {
		switch name {
		case "Title":
			_ = t.conn.Emit(itemPath, itemInterface+".NewTitle")
		case "IconName":
			_ = t.conn.Emit(itemPath, itemInterface+".NewIcon")
		case "ToolTip":
			_ = t.conn.Emit(itemPath, itemInterface+".NewToolTip")
		case "XAyatanaLabel":
			_ = t.conn.Emit(itemPath, itemInterface+".XAyatanaNewLabel", icon.Label, "")
		}
	}
	t.menu.set(items)
}

// Close stops following the watcher. The objects go away with the
// connection.
func (t *Tray) Close() {
	t.closeOnce.Do(func() {
		t.conn.RemoveSignal(t.signals)
		close(t.done)
	})
}
