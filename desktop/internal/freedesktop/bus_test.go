package freedesktop

import (
	"bufio"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

// privateBus starts a dbus-daemon of its own for one test and returns its
// address. The surfaces here are only worth testing against a real bus: what
// breaks them is a wrong signature or a signal on the wrong path, which no
// fake in Go would catch.
func privateBus(t *testing.T) string {
	t.Helper()
	daemon, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Skip("dbus-daemon not installed")
	}
	cmd := exec.Command(daemon, "--session", "--nofork", "--print-address=1",
		"--address=unix:dir="+t.TempDir())
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Skipf("dbus-daemon: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		t.Fatalf("dbus-daemon printed no address: %v", err)
	}
	return strings.TrimSpace(line)
}

func connect(t *testing.T, address string) *dbus.Conn {
	t.Helper()
	conn, err := dbus.Connect(address)
	if err != nil {
		t.Fatalf("connecting to the private bus: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// signals collects the signals a connection receives.
type signals struct {
	ch chan *dbus.Signal
}

func listen(t *testing.T, conn *dbus.Conn, opts ...dbus.MatchOption) *signals {
	t.Helper()
	if err := conn.AddMatchSignal(opts...); err != nil {
		t.Fatal(err)
	}
	s := &signals{ch: make(chan *dbus.Signal, 64)}
	conn.Signal(s.ch)
	return s
}

// next waits for the next signal with the given name, skipping others.
func (s *signals) next(t *testing.T, name string) *dbus.Signal {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case sig := <-s.ch:
			if sig.Name == name {
				return sig
			}
		case <-deadline:
			t.Fatalf("no %s signal", name)
			return nil
		}
	}
}

// none checks that no signal with the given name arrives for a moment.
func (s *signals) none(t *testing.T, name string) {
	t.Helper()
	deadline := time.After(300 * time.Millisecond)
	for {
		select {
		case sig := <-s.ch:
			if sig.Name == name {
				t.Fatalf("unexpected %s signal: %v", name, sig.Body)
			}
		case <-deadline:
			return
		}
	}
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// ------------------------------------------------------------- the service

func TestServiceCallsAndProperties(t *testing.T) {
	addr := privateBus(t)
	app, client := connect(t, addr), connect(t, addr)

	var mu sync.Mutex
	var calls []string
	record := func(s string) func() {
		return func() { mu.Lock(); calls = append(calls, s); mu.Unlock() }
	}
	svc, err := ExportService(app, Controls{
		Activate:     record("activate"),
		Connect:      record("connect"),
		Disconnect:   record("disconnect"),
		CycleANCMode: record("cycle"),
		SetANCMode: func(mode int) {
			mu.Lock()
			calls = append(calls, "mode "+string(rune('0'+mode)))
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := Claim(app); err != nil {
		t.Fatalf("first claim: %v", err)
	}

	// The second instance forwards instead of running.
	if err := Claim(client); err != ErrAlreadyRunning {
		t.Fatalf("second claim: got %v, want ErrAlreadyRunning", err)
	}
	if err := CallActivate(client); err != nil {
		t.Fatal(err)
	}
	if err := CallSetANCMode(client, 2); err != nil {
		t.Fatal(err)
	}
	if err := CallSetANCMode(client, 7); err == nil {
		t.Fatal("mode 7 accepted; the earbuds know 0, 1 and 2")
	}
	obj := client.Object(BusName, ObjectPath)
	for _, m := range []string{"Connect", "Disconnect", "CycleANCMode"} {
		if err := obj.Call(ControlInterface+"."+m, 0).Err; err != nil {
			t.Fatalf("%s: %v", m, err)
		}
	}
	waitFor(t, "every call", func() bool { mu.Lock(); defer mu.Unlock(); return len(calls) == 5 })

	// Unknown values start at -1, not at a plausible 0.
	v, err := obj.GetProperty(ControlInterface + ".BatteryLeft")
	if err != nil || v.Value() != int32(-1) {
		t.Fatalf("initial BatteryLeft = %v, %v; want -1", v, err)
	}

	sigs := listen(t, client, dbus.WithMatchObjectPath(ObjectPath),
		dbus.WithMatchInterface(propertiesInterface))
	st := Status{Known: true, Connected: true, DeviceName: "SOUNDPEATS H3",
		ANCMode: 1, ANCSupported: true, BatteryLeft: 80, BatteryRight: 75, BatteryCase: -1}
	svc.Update(st)
	sig := sigs.next(t, propertiesInterface+".PropertiesChanged")
	changed := sig.Body[1].(map[string]dbus.Variant)
	if changed["ANCMode"].Value() != int32(1) || changed["DeviceName"].Value() != "SOUNDPEATS H3" {
		t.Fatalf("PropertiesChanged carried %v", changed)
	}
	if _, ok := changed["BatteryCase"]; ok {
		t.Fatal("BatteryCase did not change but was announced")
	}

	// The same status again announces nothing.
	svc.Update(st)
	sigs.none(t, propertiesInterface+".PropertiesChanged")

	var all map[string]dbus.Variant
	if err := obj.Call(propertiesInterface+".GetAll", 0, ControlInterface).Store(&all); err != nil {
		t.Fatal(err)
	}
	if len(all) != 9 || all["Connected"].Value() != true {
		t.Fatalf("GetAll = %v", all)
	}
	if err := obj.Call(propertiesInterface+".Set", 0, ControlInterface, "Connected",
		dbus.MakeVariant(false)).Err; err == nil {
		t.Fatal("a client could write Connected")
	}
}

// ---------------------------------------------------------------- the tray

// fakeWatcher stands in for the AppIndicator extension's watcher.
type fakeWatcher struct {
	mu    sync.Mutex
	items []string
}

func startWatcher(t *testing.T, conn *dbus.Conn) *fakeWatcher {
	t.Helper()
	w := &fakeWatcher{}
	err := conn.ExportMethodTable(map[string]any{
		"RegisterStatusNotifierItem": func(sender dbus.Sender, service string) *dbus.Error {
			w.mu.Lock()
			w.items = append(w.items, string(sender)+service)
			w.mu.Unlock()
			return nil
		},
	}, watcherPath, watcherInterface)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exportProperties(conn, watcherPath, map[string]map[string]any{
		watcherInterface: {"IsStatusNotifierHostRegistered": true},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.RequestName(watcherName, dbus.NameFlagDoNotQueue); err != nil {
		t.Fatal(err)
	}
	return w
}

func (w *fakeWatcher) registrations() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.items...)
}

func TestTrayRegistersAndServesItsMenu(t *testing.T) {
	addr := privateBus(t)
	app, host := connect(t, addr), connect(t, addr)

	// No watcher yet: the tray is not visible, and closing the window must
	// not hide it.
	tray, err := NewTray(app, "openpeats", TrayIcon{IconName: "audio-headphones-symbolic", Title: "OpenPeats"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tray.Close()
	if tray.Visible() {
		t.Fatal("visible with no watcher on the bus")
	}

	// A watcher appearing later gets the item: the shell restarted, or the
	// extension was switched on.
	w := startWatcher(t, host)
	waitFor(t, "registration", func() bool { return len(w.registrations()) == 1 })
	if got, want := w.registrations()[0], app.Names()[0]+string(itemPath); got != want {
		t.Fatalf("registered %q, want %q", got, want)
	}
	waitFor(t, "visibility", tray.Visible)

	clicked := make(chan string, 4)
	items := []MenuItem{
		{Label: "SOUNDPEATS_H3", Disabled: true},
		{Separator: true},
		{Label: "Off", Radio: true, OnClick: func() { clicked <- "off" }},
		{Label: "Noise cancelling", Radio: true, Checked: true, OnClick: func() { clicked <- "anc" }},
	}
	tray.Update(TrayIcon{IconName: "audio-headphones-symbolic", Title: "OpenPeats", Label: "80%"}, items)

	menuObj := host.Object(app.Names()[0], menuPath)
	var revision uint32
	var layout menuLayout
	if err := menuObj.Call(menuInterface+".GetLayout", 0, int32(0), int32(-1), []string{}).
		Store(&revision, &layout); err != nil {
		t.Fatalf("GetLayout: %v", err)
	}
	if len(layout.Children) != 4 {
		t.Fatalf("layout has %d children, want 4", len(layout.Children))
	}
	var first menuLayout
	if err := dbus.Store([]any{layout.Children[0].Value()}, &first); err != nil {
		t.Fatalf("child layout: %v", err)
	}
	if first.ID != 1 || first.Properties["label"].Value() != "SOUNDPEATS__H3" ||
		first.Properties["enabled"].Value() != false {
		t.Fatalf("first item = %+v", first)
	}

	if err := menuObj.Call(menuInterface+".Event", 0, int32(4), "clicked",
		dbus.MakeVariant(""), uint32(0)).Err; err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-clicked:
		if got != "anc" {
			t.Fatalf("clicked %q", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the click never reached the item")
	}
	// A disabled item ignores clicks.
	_ = menuObj.Call(menuInterface+".Event", 0, int32(1), "clicked", dbus.MakeVariant(""), uint32(0))

	sigs := listen(t, host, dbus.WithMatchSender(app.Names()[0]))

	// The mode changed: same shape, so the host is told which properties.
	items[2].Checked, items[3].Checked = true, false
	tray.Update(TrayIcon{IconName: "audio-headphones-symbolic", Title: "OpenPeats", Label: "75%"}, items)
	label := sigs.next(t, itemInterface+".XAyatanaNewLabel")
	if label.Body[0] != "75%" {
		t.Fatalf("XAyatanaNewLabel = %v", label.Body)
	}
	sig := sigs.next(t, menuInterface+".ItemsPropertiesUpdated")
	var updated []menuItemProperties
	if err := dbus.Store([]any{sig.Body[0]}, &updated); err != nil {
		t.Fatal(err)
	}
	if len(updated) != 2 || updated[0].ID != 3 || updated[0].Properties["toggle-state"].Value() != int32(1) {
		t.Fatalf("ItemsPropertiesUpdated = %+v", updated)
	}

	// An item more: the layout itself changed.
	tray.Update(TrayIcon{IconName: "audio-headphones-symbolic", Title: "OpenPeats", Label: "75%"},
		append(items, MenuItem{Label: "Quit"}))
	sig = sigs.next(t, menuInterface+".LayoutUpdated")
	if sig.Body[0].(uint32) <= revision {
		t.Fatalf("revision %v did not move past %d", sig.Body[0], revision)
	}
	select {
	case got := <-clicked:
		t.Fatalf("the disabled item ran %q", got)
	default:
	}
}

// ------------------------------------------------------- the battery level

// fakeBlueZ answers what the provider asks of bluetoothd, and reads the
// provider back the way bluetoothd does.
type fakeBlueZ struct {
	conn     *dbus.Conn
	provider chan dbus.Sender
}

func startBlueZ(t *testing.T, conn *dbus.Conn) *fakeBlueZ {
	t.Helper()
	b := &fakeBlueZ{conn: conn, provider: make(chan dbus.Sender, 4)}
	objects := managedObjects{
		"/org/bluez/hci0": {"org.bluez.Adapter1": {}},
		"/org/bluez/hci0/dev_98_80_BB_40_D4_ED": {"org.bluez.Device1": {
			"Address": dbus.MakeVariant("98:80:BB:40:D4:ED"),
			"Adapter": dbus.MakeVariant(dbus.ObjectPath("/org/bluez/hci0")),
		}},
		"/org/bluez/hci0/dev_AA_AA_AA_AA_AA_AA": {
			"org.bluez.Device1": {
				"Address": dbus.MakeVariant("AA:AA:AA:AA:AA:AA"),
				"Adapter": dbus.MakeVariant(dbus.ObjectPath("/org/bluez/hci0")),
			},
			"org.bluez.Battery1": {"Percentage": dbus.MakeVariant(byte(90))},
		},
	}
	err := conn.ExportMethodTable(map[string]any{
		"GetManagedObjects": func() (managedObjects, *dbus.Error) { return objects, nil },
	}, "/", objectManagerInterface)
	if err != nil {
		t.Fatal(err)
	}
	err = conn.ExportMethodTable(map[string]any{
		"RegisterBatteryProvider": func(sender dbus.Sender, root dbus.ObjectPath) *dbus.Error {
			if root != batteryRoot {
				return dbus.MakeFailedError(nil)
			}
			b.provider <- sender
			return nil
		},
	}, "/org/bluez/hci0", batteryManagerInterface)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.RequestName("org.bluez", dbus.NameFlagDoNotQueue); err != nil {
		t.Fatal(err)
	}
	return b
}

func (b *fakeBlueZ) batteries(t *testing.T, provider dbus.Sender) managedObjects {
	t.Helper()
	var objects managedObjects
	err := b.conn.Object(string(provider), batteryRoot).
		Call(objectManagerInterface+".GetManagedObjects", 0).Store(&objects)
	if err != nil {
		t.Fatal(err)
	}
	return objects
}

func TestBatteryProviderPublishesThroughBlueZ(t *testing.T) {
	addr := privateBus(t)
	t.Setenv("DBUS_SYSTEM_BUS_ADDRESS", addr)
	bluez := startBlueZ(t, connect(t, addr))

	p, err := NewBatteryProvider()
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	// An address BlueZ does not know publishes nothing, and neither does
	// a device with a level of its own: BlueZ keeps one source per device.
	p.Set("00:11:22:33:44:55", 50)
	p.Set("AA:AA:AA:AA:AA:AA", 50)
	select {
	case <-bluez.provider:
		t.Fatal("registered for a device that reports its own level")
	case <-time.After(300 * time.Millisecond):
	}

	p.Set("98:80:bb:40:d4:ed", 80)
	var provider dbus.Sender
	select {
	case provider = <-bluez.provider:
	case <-time.After(3 * time.Second):
		t.Fatal("the provider never registered with the adapter")
	}

	path := batteryRoot + "/dev_98_80_BB_40_D4_ED"
	objs := bluez.batteries(t, provider)
	props := objs[path][batteryProviderInterface]
	if props["Percentage"].Value() != byte(80) ||
		props["Device"].Value() != dbus.ObjectPath("/org/bluez/hci0/dev_98_80_BB_40_D4_ED") {
		t.Fatalf("battery object = %v", objs)
	}

	sigs := listen(t, bluez.conn, dbus.WithMatchSender(string(provider)))
	p.Set("98:80:BB:40:D4:ED", 75)
	sig := sigs.next(t, propertiesInterface+".PropertiesChanged")
	if sig.Path != path || sig.Body[1].(map[string]dbus.Variant)["Percentage"].Value() != byte(75) {
		t.Fatalf("PropertiesChanged on %s: %v", sig.Path, sig.Body)
	}

	// The link went down: the level is withdrawn, not left to go stale.
	p.Set("98:80:BB:40:D4:ED", -1)
	sig = sigs.next(t, objectManagerInterface+".InterfacesRemoved")
	if sig.Body[0] != path {
		t.Fatalf("InterfacesRemoved for %v", sig.Body[0])
	}
	if objs := bluez.batteries(t, provider); len(objs) != 0 {
		t.Fatalf("still published after withdrawal: %v", objs)
	}

	// Back again: the provider is registered already, so the object is
	// announced rather than registered a second time.
	p.Set("98:80:BB:40:D4:ED", 70)
	sig = sigs.next(t, objectManagerInterface+".InterfacesAdded")
	if sig.Body[0] != path {
		t.Fatalf("InterfacesAdded for %v", sig.Body[0])
	}
	select {
	case <-bluez.provider:
		t.Fatal("registered twice with the same adapter")
	default:
	}
}

func TestDeviceByAddressIgnoresCase(t *testing.T) {
	objects := managedObjects{
		"/org/bluez/hci0/dev_AA": {"org.bluez.Device1": {
			"Address": dbus.MakeVariant("AA:BB:CC:DD:EE:FF"),
			"Adapter": dbus.MakeVariant(dbus.ObjectPath("/org/bluez/hci0")),
		}},
		"/org/bluez/hci0": {"org.bluez.Adapter1": {}},
	}
	dev, adapter, ok := deviceByAddress(objects, "aa:bb:cc:dd:ee:ff")
	if !ok || dev != "/org/bluez/hci0/dev_AA" || adapter != "/org/bluez/hci0" {
		t.Fatalf("got %q %q %v", dev, adapter, ok)
	}
	if _, _, ok := deviceByAddress(objects, "11:22:33:44:55:66"); ok {
		t.Fatal("found a device that is not there")
	}
}
