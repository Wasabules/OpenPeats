package freedesktop

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
)

// The control service: one name on the session bus that the GNOME Shell
// extension, the launcher's actions and a second launch of the application
// all talk to. Owning the name is also what keeps OpenPeats to one instance —
// the earbuds take a single control link, so two instances would fight over
// it.

const (
	// BusName is the well-known name of a running OpenPeats.
	BusName = "io.github.wasabules.OpenPeats"
	// ObjectPath is where the control interface lives.
	ObjectPath = dbus.ObjectPath("/io/github/wasabules/OpenPeats")
	// ControlInterface is versioned: a change that breaks a client gets a
	// new interface rather than a silent change of this one.
	ControlInterface = "io.github.wasabules.OpenPeats1"
)

// Controls is what a client may ask for. Each runs on its own goroutine and
// the call returns at once: connecting takes seconds, longer than a client
// should wait on a reply. The outcome shows in the published Status.
type Controls struct {
	// Activate shows the window.
	Activate func()
	// Connect opens the link to the remembered earbuds.
	Connect func()
	// Disconnect closes it.
	Disconnect func()
	// SetANCMode switches the noise control mode, connecting first if
	// needed: 0 off, 1 noise cancelling, 2 transparency.
	SetANCMode func(mode int)
	// CycleANCMode moves to the next mode, as a one-button control does.
	CycleANCMode func()
}

// Status is what the service publishes as properties. Unknown numbers are -1.
type Status struct {
	// Known reports that earbuds were connected once, so Connect has an
	// address to go to.
	Known      bool
	Connected  bool
	Connecting bool
	DeviceName string
	ANCMode    int
	// ANCSupported is false once the earbuds have shown they have no
	// noise control; it stays true while that is not known.
	ANCSupported bool
	BatteryLeft  int
	BatteryRight int
	BatteryCase  int
}

func (s Status) properties() map[string]any {
	return map[string]any{
		"Known":        s.Known,
		"Connected":    s.Connected,
		"Connecting":   s.Connecting,
		"DeviceName":   s.DeviceName,
		"ANCMode":      int32(s.ANCMode),
		"ANCSupported": s.ANCSupported,
		"BatteryLeft":  int32(s.BatteryLeft),
		"BatteryRight": int32(s.BatteryRight),
		"BatteryCase":  int32(s.BatteryCase),
	}
}

// ServiceIntrospection describes the control interface. The GNOME Shell
// extension carries a copy of the interface element; a change here goes
// there too.
const ServiceIntrospection = `<node>
 <interface name="io.github.wasabules.OpenPeats1">
  <method name="Activate"/>
  <method name="Connect"/>
  <method name="Disconnect"/>
  <method name="SetANCMode"><arg name="mode" type="i" direction="in"/></method>
  <method name="CycleANCMode"/>
  <property name="Known" type="b" access="read"/>
  <property name="Connected" type="b" access="read"/>
  <property name="Connecting" type="b" access="read"/>
  <property name="DeviceName" type="s" access="read"/>
  <property name="ANCMode" type="i" access="read"/>
  <property name="ANCSupported" type="b" access="read"/>
  <property name="BatteryLeft" type="i" access="read"/>
  <property name="BatteryRight" type="i" access="read"/>
  <property name="BatteryCase" type="i" access="read"/>
 </interface>` + introspect.IntrospectDataString + propertiesIntrospection + `</node>`

// Service is the exported control interface.
type Service struct {
	props *properties
}

// ExportService puts the control interface on conn. Export it before
// claiming the name: a client that started OpenPeats through D-Bus activation
// has its call delivered the moment the name is taken, and a call reaching an
// object not exported yet would fail.
func ExportService(conn *dbus.Conn, c Controls) (*Service, error) {
	run := func(f func()) func() *dbus.Error {
		return func() *dbus.Error {
			if f != nil {
				go f()
			}
			return nil
		}
	}
	err := conn.ExportMethodTable(map[string]any{
		"Activate":     run(c.Activate),
		"Connect":      run(c.Connect),
		"Disconnect":   run(c.Disconnect),
		"CycleANCMode": run(c.CycleANCMode),
		"SetANCMode": func(mode int32) *dbus.Error {
			if mode < 0 || mode > 2 {
				return dbus.NewError("org.freedesktop.DBus.Error.InvalidArgs",
					[]any{fmt.Sprintf("mode %d: expected 0, 1 or 2", mode)})
			}
			if c.SetANCMode != nil {
				go c.SetANCMode(int(mode))
			}
			return nil
		},
	}, ObjectPath, ControlInterface)
	if err != nil {
		return nil, err
	}
	props, err := exportProperties(conn, ObjectPath, map[string]map[string]any{
		ControlInterface: Status{ANCMode: -1, ANCSupported: true,
			BatteryLeft: -1, BatteryRight: -1, BatteryCase: -1}.properties(),
	})
	if err != nil {
		return nil, err
	}
	if err := conn.Export(introspect.Introspectable(ServiceIntrospection), ObjectPath,
		"org.freedesktop.DBus.Introspectable"); err != nil {
		return nil, err
	}
	return &Service{props: props}, nil
}

// Update publishes a new status; clients see what changed.
func (s *Service) Update(st Status) {
	s.props.update(ControlInterface, st.properties())
}

// ErrAlreadyRunning is what Claim reports when another process owns the
// name.
var ErrAlreadyRunning = errors.New("OpenPeats is already running")

// Claim takes the well-known name for this process. It fails with
// ErrAlreadyRunning when another instance holds it; the caller then forwards
// its request with the Call* functions and exits.
func Claim(conn *dbus.Conn) error {
	reply, err := conn.RequestName(BusName, dbus.NameFlagDoNotQueue)
	if err != nil {
		return err
	}
	switch reply {
	case dbus.RequestNameReplyPrimaryOwner, dbus.RequestNameReplyAlreadyOwner:
		return nil
	default:
		return ErrAlreadyRunning
	}
}

// remote calls a method of the running instance.
func remote(conn *dbus.Conn, method string, args ...any) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return conn.Object(BusName, ObjectPath).
		CallWithContext(ctx, ControlInterface+"."+method, 0, args...).Err
}

// CallActivate asks the running instance to show its window.
func CallActivate(conn *dbus.Conn) error { return remote(conn, "Activate") }

// CallSetANCMode asks the running instance to switch the noise control mode.
func CallSetANCMode(conn *dbus.Conn, mode int) error {
	return remote(conn, "SetANCMode", int32(mode))
}

// ShellExtensionActive reports whether GNOME Shell runs the given extension.
// Any error — another desktop, an older shell — reads as no.
func ShellExtensionActive(conn *dbus.Conn, uuid string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var info map[string]dbus.Variant
	err := conn.Object(shellExtensionsName, shellExtensionsPath).
		CallWithContext(ctx, shellExtensionsInterface+".GetExtensionInfo", dbus.FlagNoAutoStart, uuid).
		Store(&info)
	if err != nil {
		return false
	}
	// 1 is ENABLED up to GNOME 44 and ACTIVE from 45: the same value under
	// two names, meaning the extension is running.
	state, _ := info["state"].Value().(float64)
	return state == 1
}
