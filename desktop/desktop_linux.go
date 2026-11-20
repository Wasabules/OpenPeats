//go:build linux

package main

import (
	"errors"
	"log"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/godbus/dbus/v5"

	"openpeats/internal/freedesktop"
	"openpeats/internal/i18n"
	"openpeats/internal/launch"
	"openpeats/internal/peats"
)

// The Linux desktop around the window:
//
//   - the control service on the session bus, which the GNOME Shell extension
//     and the launcher's actions talk to, and which keeps OpenPeats to one
//     instance;
//   - an icon in the panel with a menu, drawn by GNOME's AppIndicator
//     extension or by any StatusNotifierItem host;
//   - the battery level, handed to BlueZ so GNOME's own menus show it.
//
// All three show the same state and act through the same few paths, so a
// mode picked in the panel and one picked in the quick settings behave alike.

// extensionUUID is the GNOME Shell extension in desktop/gnome-extension.
const extensionUUID = "openpeats@wasabules.github.io"

// trayIconName is drawn in the panel: a symbolic icon matches the others there.
const trayIconName = "audio-headphones-symbolic"

type linuxDesktop struct {
	app  *App
	opts launch.Options
	bus  *dbus.Conn

	service *freedesktop.Service

	// ready closes once Wails is up: a request arriving earlier, as one
	// that started the process through D-Bus activation does, waits for it.
	ready chan struct{}

	// publishMu serialises publishing, and guards tray and battery, which
	// start sets after the service is already live.
	publishMu sync.Mutex
	tray      *freedesktop.Tray
	battery   *freedesktop.BatteryProvider

	// connectMu serialises the connections asked for from outside the
	// window, so that two quick clicks do not dial twice.
	connectMu  sync.Mutex
	connecting atomic.Bool

	// modeMu serialises mode changes: each resolves against what the one
	// before it left.
	modeMu sync.Mutex

	nameMu    sync.Mutex
	names     map[string]string // address → name, as BlueZ knows it
	resolving map[string]bool
}

// newDesktop takes the control service's name. When another instance holds it,
// the request on this command line goes to that instance and forwarded is true:
// this process has nothing left to do.
func newDesktop(app *App, opts launch.Options) (shell desktopShell, forwarded bool) {
	bus, err := dbus.ConnectSessionBus()
	if err != nil {
		log.Printf("desktop: no session bus, no panel integration: %v", err)
		return noDesktop{}, false
	}
	d := &linuxDesktop{
		app:       app,
		opts:      opts,
		bus:       bus,
		ready:     make(chan struct{}),
		names:     map[string]string{},
		resolving: map[string]bool{},
	}

	d.service, err = freedesktop.ExportService(bus, freedesktop.Controls{
		Activate:   d.whenReady(app.showWindow),
		Connect:    d.whenReady(func() { d.ensureConnected(true) }),
		Disconnect: d.whenReady(app.Disconnect),
		SetANCMode: func(mode int) {
			d.whenReady(func() { d.setMode(func(int) int { return mode }) })()
		},
		CycleANCMode: d.whenReady(func() { d.setMode(nextANCMode) }),
	})
	if err != nil {
		log.Printf("desktop: control service not exported: %v", err)
		bus.Close()
		return noDesktop{}, false
	}

	switch err := freedesktop.Claim(bus); {
	case errors.Is(err, freedesktop.ErrAlreadyRunning):
		d.forward()
		bus.Close()
		return nil, true
	case err != nil:
		// Running twice is the lesser evil next to not running at all.
		log.Printf("desktop: %s not claimed, a second instance will not be caught: %v",
			freedesktop.BusName, err)
	}
	return d, false
}

// forward hands this command line to the instance already running.
func (d *linuxDesktop) forward() {
	var err error
	switch {
	case d.opts.ANCMode >= 0:
		err = freedesktop.CallSetANCMode(d.bus, d.opts.ANCMode)
	case d.opts.Background:
		return // it runs already, which is all this asked for
	default:
		err = freedesktop.CallActivate(d.bus)
	}
	if err != nil {
		log.Printf("desktop: the running instance did not answer: %v", err)
	}
}

// whenReady defers f until Wails is up. The service runs each request on its
// own goroutine, so waiting here blocks nobody.
func (d *linuxDesktop) whenReady(f func()) func() {
	return func() {
		<-d.ready
		f()
	}
}

func (d *linuxDesktop) start() {
	tray, err := freedesktop.NewTray(d.bus, "openpeats",
		freedesktop.TrayIcon{IconName: trayIconName, Title: "OpenPeats"}, d.app.showWindow)
	if err != nil {
		log.Printf("desktop: no panel icon: %v", err)
	}
	// No system bus or no BlueZ only costs the battery in GNOME's menus.
	battery, err := freedesktop.NewBatteryProvider()
	if err != nil {
		log.Printf("desktop: battery level not handed to BlueZ: %v", err)
	}

	d.publishMu.Lock()
	d.tray, d.battery = tray, battery
	d.publishMu.Unlock()
	close(d.ready)
	d.publish()

	switch {
	case d.opts.ANCMode >= 0:
		mode := d.opts.ANCMode
		go d.setMode(func(int) int { return mode })
	case d.opts.Background:
		// Started with the session, or by a client of the service: the
		// earbuds may well be in their case. A failure is no news.
		go d.ensureConnected(false)
	}
}

func (d *linuxDesktop) stateChanged() { d.publish() }

func (d *linuxDesktop) hasIndicator() bool {
	d.publishMu.Lock()
	tray := d.tray
	d.publishMu.Unlock()
	if tray != nil && tray.Visible() {
		return true
	}
	if tray == nil && freedesktop.TrayHostAvailable(d.bus) { // before start
		return true
	}
	return freedesktop.ShellExtensionActive(d.bus, extensionUUID)
}

// closedToBackground says once, the first time, where the application went.
func (d *linuxDesktop) closedToBackground() {
	if loadSettings().BackgroundHintShown {
		return
	}
	if err := updateSettings(func(s *settings) { s.BackgroundHintShown = true }); err != nil {
		log.Printf("settings: %v", err)
	}
	d.app.notify(i18n.T("tray.backgroundHint.title"), i18n.T("tray.backgroundHint.body"), false)
}

func (d *linuxDesktop) close() {
	d.publishMu.Lock()
	defer d.publishMu.Unlock()
	if d.tray != nil {
		d.tray.Close()
	}
	if d.battery != nil {
		d.battery.Close()
	}
	d.bus.Close()
}

// ----------------------------------------------------------------- actions

// ensureConnected opens the link to the remembered earbuds unless it is up.
// loud is for a request someone just made: with the window hidden, a
// notification is the only place left to say why it failed.
func (d *linuxDesktop) ensureConnected(loud bool) bool {
	d.connectMu.Lock()
	defer d.connectMu.Unlock()

	if d.app.conn() != nil {
		return true
	}
	mac := d.app.GetState().MAC
	if mac == "" {
		// No earbuds picked yet: that happens in the window.
		if loud {
			d.app.showWindow()
		}
		return false
	}

	d.connecting.Store(true)
	d.publish()
	err := d.app.connect(mac)
	d.connecting.Store(false)
	d.publish()

	if err != nil {
		if loud {
			d.app.notify(i18n.T("tray.connectFailed"), connectErrorText(err), false)
		}
		return false
	}
	return true
}

// setMode switches the noise control to what pick returns for the current
// mode, connecting first if needed.
//
// The mode is read before anything is decided: a gesture on the earbuds
// changes it without telling anyone, so the state shown may be behind. Picking
// the mode already shown then reads, finds it set, and writes nothing — a
// write would replay the voice prompt for no change.
func (d *linuxDesktop) setMode(pick func(current int) int) {
	if !d.ensureConnected(true) {
		return
	}
	d.modeMu.Lock()
	defer d.modeMu.Unlock()

	d.app.read(peats.CmdANCModeGet)
	current := d.app.GetState().ANCMode
	if target := pick(current); target != current {
		d.app.SetANCMode(target)
	}
}

// ------------------------------------------------------------- what shows

// desktopView is the state as the desktop shows it.
type desktopView struct {
	known      bool // earbuds were connected once
	connected  bool
	connecting bool
	name       string
	mode       int  // -1: unknown
	ancAbsent  bool // the earbuds showed they have no noise control
	left       int  // -1: unknown
	right      int
	caseLevel  int
	mac        string
}

func (d *linuxDesktop) view() desktopView {
	s := d.app.GetState()
	v := desktopView{
		known:      s.MAC != "",
		connected:  s.Connected,
		connecting: d.connecting.Load() && !s.Connected,
		name:       d.deviceName(s.MAC),
		mode:       -1,
		left:       -1,
		right:      -1,
		caseLevel:  -1,
		mac:        s.MAC,
	}
	if !s.Connected {
		return v
	}
	supported, probed := s.Supported["ancMode"]
	v.ancAbsent = probed && !supported
	if supported {
		v.mode = s.ANCMode
	}
	level := func(value int, feature string) int {
		if !s.Supported[feature] || value < 0 || value > 100 {
			return -1
		}
		return value
	}
	v.left = level(s.BatteryLeft, "batteryLeft")
	v.right = level(s.BatteryRight, "batteryRight")
	v.caseLevel = level(s.BatteryCase, "batteryCase")
	return v
}

// weakest is the lower of the two earbuds, the one that runs out first; -1
// when neither is known. An earbud at 0 is taken as absent rather than
// empty — one that reports at all is not flat.
func (v desktopView) weakest() int {
	w := -1
	for _, level := range []int{v.left, v.right} {
		if level > 0 && (w < 0 || level < w) {
			w = level
		}
	}
	return w
}

// percent renders a level the way the language writes it: 80% or 80 %.
func percent(level int) string {
	return i18n.T("value.percent", "value", level)
}

// status is the first line: what is connected, or what the link is doing.
func (v desktopView) status() string {
	switch {
	case !v.known:
		return i18n.T("tray.noEarbuds")
	case v.connecting:
		return i18n.T("tray.connecting")
	case !v.connected:
		return i18n.T("tray.disconnected")
	case v.name != "":
		return v.name
	default:
		return i18n.T("tray.connected")
	}
}

// batteries is the levels on one line, empty when none is known.
func (v desktopView) batteries() string {
	var parts []string
	for _, b := range []struct {
		key   string
		level int
	}{{"tray.battery.left", v.left}, {"tray.battery.right", v.right}, {"tray.battery.case", v.caseLevel}} {
		if b.level >= 0 {
			parts = append(parts, i18n.T(b.key, "percent", percent(b.level)))
		}
	}
	return strings.Join(parts, " · ")
}

func (d *linuxDesktop) publish() {
	d.publishMu.Lock()
	defer d.publishMu.Unlock()

	v := d.view()
	d.service.Update(freedesktop.Status{
		Known:        v.known,
		Connected:    v.connected,
		Connecting:   v.connecting,
		DeviceName:   v.name,
		ANCMode:      v.mode,
		ANCSupported: !v.ancAbsent,
		BatteryLeft:  v.left,
		BatteryRight: v.right,
		BatteryCase:  v.caseLevel,
	})

	if d.tray != nil {
		icon := freedesktop.TrayIcon{
			IconName: trayIconName,
			Title:    "OpenPeats",
			Tooltip:  strings.TrimSpace(v.status() + "\n" + v.batteries()),
		}
		if w := v.weakest(); w >= 0 {
			icon.Label = percent(w)
		}
		d.tray.Update(icon, d.menu(v))
	}

	if d.battery != nil {
		level := -1
		if v.connected {
			level = v.weakest()
		}
		d.battery.Set(v.mac, level)
	}
}

// menu is the panel icon's menu for a view.
func (d *linuxDesktop) menu(v desktopView) []freedesktop.MenuItem {
	items := []freedesktop.MenuItem{{Label: v.status(), Disabled: true}}
	if line := v.batteries(); line != "" {
		items = append(items, freedesktop.MenuItem{Label: line, Disabled: true})
	}

	// The modes can be picked while disconnected: picking one connects
	// first, as the quick settings and the Android tile do.
	items = append(items, freedesktop.MenuItem{Separator: true})
	for _, m := range []struct {
		code int
		key  string
	}{
		{int(peats.ANCOff), "tray.mode.off"},
		{int(peats.ANCOn), "tray.mode.anc"},
		{int(peats.ANCTransparency), "tray.mode.transparency"},
	} {
		code := m.code
		items = append(items, freedesktop.MenuItem{
			Label:    i18n.T(m.key),
			Radio:    true,
			Checked:  v.mode == code,
			Disabled: !v.known || v.ancAbsent,
			OnClick:  func() { d.setMode(func(int) int { return code }) },
		})
	}

	items = append(items, freedesktop.MenuItem{Separator: true})
	switch {
	case v.connected:
		items = append(items, freedesktop.MenuItem{
			Label: i18n.T("tray.disconnect"), OnClick: d.app.Disconnect})
	case v.known:
		items = append(items, freedesktop.MenuItem{
			Label: i18n.T("tray.connect"), Disabled: v.connecting,
			OnClick: func() { d.ensureConnected(true) }})
	}
	return append(items,
		freedesktop.MenuItem{Label: i18n.T("tray.open"), OnClick: d.app.showWindow},
		freedesktop.MenuItem{Separator: true},
		freedesktop.MenuItem{Label: i18n.T("tray.quit"), OnClick: d.app.quit},
	)
}

// deviceName is the earbuds' name as the system knows it, looked up once per
// address. Empty until the lookup is back; it publishes again then.
func (d *linuxDesktop) deviceName(mac string) string {
	if mac == "" {
		return ""
	}
	d.nameMu.Lock()
	defer d.nameMu.Unlock()
	if name, ok := d.names[mac]; ok {
		return name
	}
	if !d.resolving[mac] {
		d.resolving[mac] = true
		go d.resolveName(mac)
	}
	return ""
}

func (d *linuxDesktop) resolveName(mac string) {
	name := ""
	if devices, err := peats.PairedAudioDevices(); err == nil {
		for _, dev := range devices {
			if strings.EqualFold(dev.Address, mac) {
				name = dev.Name
			}
		}
	}
	d.nameMu.Lock()
	d.names[mac] = name
	delete(d.resolving, mac)
	d.nameMu.Unlock()
	if name != "" {
		d.publish()
	}
}
