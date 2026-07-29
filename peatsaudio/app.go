package main

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"peatsaudio/internal/i18n"
	"peatsaudio/internal/peats"
)

// DefaultMAC is the development earbuds address. Editable in the UI.
const DefaultMAC = "98:80:BB:40:D4:ED"

// State is the full device state, pushed to the frontend.
type State struct {
	Connected bool   `json:"connected"`
	MAC       string `json:"mac"`
	Channel   int    `json:"channel"`
	Firmware  string `json:"firmware"`

	BatteryLeft  int `json:"batteryLeft"`
	BatteryRight int `json:"batteryRight"`
	BatteryCase  int `json:"batteryCase"`

	ANCMode   int          `json:"ancMode"`
	ANCLevel  int          `json:"ancLevel"`
	ANCLevels []peats.Pair `json:"ancLevels"`

	TransparencyLevel  int          `json:"transparencyLevel"`
	TransparencyLevels []peats.Pair `json:"transparencyLevels"`

	GameMode      bool `json:"gameMode"`
	Multipoint    bool `json:"multipoint"`
	LDAC          bool `json:"ldac"`
	LEAudio       bool `json:"leAudio"`
	TouchDisabled bool `json:"touchDisabled"`

	PromptVolume int `json:"promptVolume"`
	VoicePack    int `json:"voicePack"`

	KeyMappings []peats.KeyMapping `json:"keyMappings"`

	// Supported records which features this model actually exposes. Filled in
	// on refresh: a command that stays silent across retries is taken as
	// absent from the hardware.
	Supported map[string]bool `json:"supported"`

	BatteryAlert  bool `json:"batteryAlert"`
	BatteryThresh int  `json:"batteryThresh"`

	// Language is the locale Go renders its own output in — desktop
	// notifications. The frontend sets it through SetLanguage so system
	// notifications match the language shown in the window.
	Language string `json:"language"`
}

// App is the application context exposed to Wails.
type App struct {
	ctx context.Context

	mu     sync.RWMutex
	client *peats.Client
	state  State

	// notified remembers which sources already raised an alert, so the
	// notification is not repeated on every poll.
	notified map[string]bool

	// wantConnected tells a dropped link apart from a requested disconnect:
	// automatic reconnection must not fight a user who just clicked
	// "Disconnect".
	wantConnected bool

	monitorStop chan struct{}
}

func NewApp() *App {
	return &App{
		state: State{
			MAC:           DefaultMAC,
			Supported:     map[string]bool{},
			BatteryAlert:  true,
			BatteryThresh: 20,
			Language:      i18n.Locale(),
		},
		notified: map[string]bool{},
	}
}

func (a *App) startup(ctx context.Context) { a.ctx = ctx }

func (a *App) shutdown(context.Context) {
	a.stopMonitor()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.client != nil {
		a.client.Close()
		a.client = nil
	}
}

func (a *App) emit(event string, data ...any) {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, event, data...)
	}
}

// log pushes a localisable line into the UI console.
func (a *App) log(key string, args ...any) {
	a.emit("log", msg(key, args...))
}

// emitFrame broadcasts a frame to the UI console. Named rather than anonymous,
// so it can be rebound identically after a reconnection.
func (a *App) emitFrame(f peats.Frame) {
	a.emit("frame", map[string]any{
		"cmd":     f.Command.String(),
		"payload": strings.ToUpper(hex.EncodeToString(f.Payload)),
		"ts":      time.Now().Format("15:04:05.000"),
	})
}

func (a *App) pushState() {
	a.mu.RLock()
	s := a.state
	a.mu.RUnlock()
	a.emit("state", s)
}

// GetState returns the current state (initial frontend call).
func (a *App) GetState() State {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.state
}

// ---------------------------------------------------------------- language

// SetLanguage switches the locale Go renders its own output in — desktop
// notifications — and returns the locale actually applied, which falls back to
// English when no catalogue matches. The frontend calls this whenever the user
// picks a language, so a notification never arrives in a different language
// from the window that triggered it.
func (a *App) SetLanguage(tag string) string {
	applied := i18n.SetLocale(tag)
	a.mu.Lock()
	a.state.Language = applied
	a.mu.Unlock()
	a.pushState()
	return applied
}

// AvailableLanguages lists the locales this build ships.
func (a *App) AvailableLanguages() []string { return i18n.Available() }

// ---------------------------------------------------------------- connection

// Connect opens the control connection and does a first refresh.
// The returned Message is empty on success.
func (a *App) Connect(mac string) Message {
	if strings.TrimSpace(mac) == "" {
		mac = DefaultMAC
	}

	a.mu.Lock()
	if a.client != nil {
		a.client.Close()
		a.client = nil
	}
	a.mu.Unlock()

	a.log("log.connecting", "mac", mac)
	cl, err := peats.Connect(mac, 0)
	if err != nil {
		reason := describeConnectError(err)
		a.log("log.connectFailed")
		a.mu.Lock()
		a.state.Connected = false
		a.mu.Unlock()
		a.pushState()
		return reason
	}

	// Every frame is mirrored into the UI log.
	cl.OnFrame = a.emitFrame

	// Several settings reboot the earbuds: without this, the link dropping
	// would go unnoticed and the interface would freeze on a dead state.
	cl.OnDisconnect = a.handleDisconnect

	a.mu.Lock()
	a.client = cl
	a.state.Connected = true
	a.state.MAC = mac
	a.state.Channel = int(cl.Channel())
	a.notified = map[string]bool{}
	a.wantConnected = true
	a.mu.Unlock()

	a.log("log.connected", "channel", cl.Channel())
	a.Refresh()
	a.startMonitor()
	return Message{}
}

// describeConnectError turns a transport error into a message the UI can
// render, keeping the actionable advice attached to each known cause.
func describeConnectError(err error) Message {
	switch {
	case errors.Is(err, peats.ErrBusy):
		return msg("error.busy")
	case errors.Is(err, peats.ErrUnreachable):
		return msg("error.unreachable")
	case errors.Is(err, peats.ErrNoChannel):
		return msg("error.noChannel")
	case errors.Is(err, peats.ErrUnsupported):
		return msg("error.unsupported")
	default:
		return msg("error.raw", "error", err.Error())
	}
}

func (a *App) Disconnect() {
	a.stopMonitor()
	a.mu.Lock()
	if a.client != nil {
		a.client.Close()
		a.client = nil
	}
	a.state.Connected = false
	a.wantConnected = false
	a.mu.Unlock()
	a.log("log.disconnected")
	a.pushState()
}

// ------------------------------------------------------- recovery after reboot

// reconnectDelay gives the earbuds time to come back up. Trying too early
// fails and burns an attempt for nothing.
const (
	reconnectDelay    = 3 * time.Second
	reconnectAttempts = 10
)

// handleDisconnect reacts to a link drop we did not ask for.
//
// LDAC, multipoint and LE Audio all reboot the earbuds: the control channel
// goes down and only comes back a few seconds later. Without this recovery the
// application sat on a dead client whose every command failed, and never said
// so.
func (a *App) handleDisconnect() {
	a.mu.Lock()
	dead := a.client
	if dead == nil { // deliberate disconnect, already handled
		a.mu.Unlock()
		return
	}
	a.client = nil
	a.state.Connected = false
	mac := a.state.MAC
	retry := a.wantConnected
	a.mu.Unlock()

	dead.Close() // the socket still needs releasing even with the link gone
	a.stopMonitor()
	a.pushState()

	if !retry {
		a.log("log.linkLost")
		return
	}
	a.log("log.linkLostReconnecting")
	go a.reconnectLoop(mac)
}

// reconnectLoop retries the connection while the earbuds reboot.
func (a *App) reconnectLoop(mac string) {
	for i := 1; i <= reconnectAttempts; i++ {
		time.Sleep(reconnectDelay)

		// The user may have disconnected or reconnected in the meantime.
		a.mu.RLock()
		abandon := !a.wantConnected || a.client != nil
		a.mu.RUnlock()
		if abandon {
			return
		}

		cl, err := peats.Connect(mac, 0)
		if err != nil {
			a.log("log.reconnectAttempt",
				"attempt", i,
				"total", reconnectAttempts,
				"reason", ref(describeConnectError(err).Key))
			continue
		}

		cl.OnFrame = a.emitFrame
		cl.OnDisconnect = a.handleDisconnect

		a.mu.Lock()
		a.client = cl
		a.state.Connected = true
		a.state.Channel = int(cl.Channel())
		a.mu.Unlock()

		a.log("log.reconnected", "channel", cl.Channel())
		a.Refresh()
		a.startMonitor()
		return
	}
	a.log("log.reconnectGaveUp", "attempts", reconnectAttempts)
}

// -------------------------------------------------------- battery monitoring

const batteryPollInterval = 90 * time.Second

func (a *App) startMonitor() {
	a.stopMonitor()

	stop := make(chan struct{})
	a.mu.Lock()
	a.monitorStop = stop
	a.mu.Unlock()

	go func() {
		t := time.NewTicker(batteryPollInterval)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				a.pollBatteries()
			}
		}
	}()
}

func (a *App) stopMonitor() {
	a.mu.Lock()
	if a.monitorStop != nil {
		close(a.monitorStop)
		a.monitorStop = nil
	}
	a.mu.Unlock()
}

// pollBatteries reads the levels back and raises an alert when one crosses the
// threshold. A 5-point hysteresis avoids flapping around the threshold.
func (a *App) pollBatteries() {
	c := a.conn()
	if c == nil {
		return
	}

	readings := []struct {
		key, labelKey string
		cmd           peats.Command
		set           func(int)
	}{
		{"left", "battery.left", peats.CmdBatteryLeft,
			func(v int) { a.state.BatteryLeft = v }},
		{"right", "battery.right", peats.CmdBatteryRight,
			func(v int) { a.state.BatteryRight = v }},
		{"case", "battery.case", peats.CmdBatteryCase,
			func(v int) { a.state.BatteryCase = v }},
	}

	a.mu.RLock()
	alert, thresh := a.state.BatteryAlert, a.state.BatteryThresh
	a.mu.RUnlock()

	changed := false
	for _, r := range readings {
		f, err := c.Get(r.cmd)
		if err != nil {
			continue
		}
		v := int(f.Scalar())
		if v > 100 { // out-of-range value: ignore rather than alert wrongly
			continue
		}
		a.mu.Lock()
		r.set(v)
		already := a.notified[r.key]
		switch {
		case v <= thresh && !already:
			a.notified[r.key] = true
			a.mu.Unlock()
			if alert {
				a.notifyLowBattery(r.labelKey, v)
			}
		case v > thresh+5 && already:
			a.notified[r.key] = false
			a.mu.Unlock()
		default:
			a.mu.Unlock()
		}
		changed = true
	}

	if changed {
		a.pushState()
	}
}

// notifyLowBattery sends a desktop notification and tells the UI.
//
// This is one of the few places Go renders text itself, so it goes through the
// Go catalogue rather than emitting a key: the notification daemon takes a
// finished string.
func (a *App) notifyLowBattery(labelKey string, pct int) {
	label := i18n.T(labelKey)
	title := i18n.T("notify.lowBattery.title")
	body := i18n.T("notify.lowBattery.body", "label", label, "percent", pct)

	a.log("log.lowBattery", "label", ref(labelKey), "percent", pct)
	a.emit("alert", map[string]any{"labelKey": labelKey, "percent": pct})

	// notify-send ships with every freedesktop desktop. Its absence is not a
	// blocking error: the alert stays visible inside the application. On
	// Windows it is simply missing, which is why the in-app banner exists.
	cmd := exec.Command("notify-send",
		"--app-name=PeatsAudio",
		"--urgency=critical",
		"--icon=audio-headset",
		title, body)
	if err := cmd.Run(); err != nil {
		a.log("log.notifyUnavailable", "error", err.Error())
	}
}

// SetBatteryAlert turns low-battery notifications on or off.
func (a *App) SetBatteryAlert(on bool) {
	a.mu.Lock()
	a.state.BatteryAlert = on
	a.mu.Unlock()
	a.pushState()
}

// SetBatteryThreshold sets the alert threshold (5–50%).
func (a *App) SetBatteryThreshold(v int) {
	if v < 5 {
		v = 5
	}
	if v > 50 {
		v = 50
	}
	a.mu.Lock()
	a.state.BatteryThresh = v
	a.notified = map[string]bool{} // threshold moved: rearm the alerts
	a.mu.Unlock()
	a.pushState()
}

// TestBatteryNotification sends a dummy notification to check the desktop
// notification channel works.
func (a *App) TestBatteryNotification() {
	a.notifyLowBattery("notify.test.label", 15)
}

func (a *App) conn() *peats.Client {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.client
}

// ------------------------------------------------------------------- reads

// Refresh reads the whole device state back and updates the support map.
func (a *App) Refresh() {
	c := a.conn()
	if c == nil {
		return
	}

	supported := map[string]bool{}

	getScalar := func(key string, cmd peats.Command) (byte, bool) {
		f, err := c.Get(cmd)
		if err != nil {
			supported[key] = false
			return 0, false
		}
		supported[key] = true
		return f.Scalar(), true
	}
	getBool := func(key string, cmd peats.Command) (bool, bool) {
		v, ok := getScalar(key, cmd)
		return v == 1, ok
	}

	a.mu.Lock()
	s := &a.state

	if v, ok := getScalar("batteryLeft", peats.CmdBatteryLeft); ok {
		s.BatteryLeft = int(v)
	}
	if v, ok := getScalar("batteryRight", peats.CmdBatteryRight); ok {
		s.BatteryRight = int(v)
	}
	if v, ok := getScalar("batteryCase", peats.CmdBatteryCase); ok {
		s.BatteryCase = int(v)
	}

	if f, err := c.Get(peats.CmdFirmware); err == nil {
		supported["firmware"] = true
		// One status byte, then the version in ASCII.
		if len(f.Payload) > 1 {
			s.Firmware = strings.TrimRight(string(f.Payload[1:]), "\x00")
		}
	} else {
		supported["firmware"] = false
	}

	if v, ok := getScalar("ancMode", peats.CmdANCModeGet); ok {
		s.ANCMode = int(v)
	}

	if f, err := c.Get(peats.CmdANCLevelGet); err == nil {
		supported["ancLevel"] = true
		s.ANCLevels = f.Pairs()
		if code, ok := f.ActiveCode(); ok {
			s.ANCLevel = int(code)
		}
	} else {
		supported["ancLevel"] = false
	}

	if f, err := c.Get(peats.CmdTransparentLevelGet); err == nil {
		supported["transparencyLevel"] = true
		s.TransparencyLevels = f.Pairs()
		if code, ok := f.ActiveCode(); ok {
			s.TransparencyLevel = int(code)
		}
	} else {
		supported["transparencyLevel"] = false
	}

	if v, ok := getBool("gameMode", peats.CmdGameModeGet); ok {
		s.GameMode = v
	}
	if v, ok := getBool("multipoint", peats.CmdMultipointGet); ok {
		s.Multipoint = v
	}
	if v, ok := getBool("ldac", peats.CmdLDACGet); ok {
		s.LDAC = v
	}
	if v, ok := getBool("leAudio", peats.CmdLEAudioGet); ok {
		s.LEAudio = v
	}
	if v, ok := getBool("touchDisabled", peats.CmdTouchDisableGet); ok {
		s.TouchDisabled = v
	}
	if v, ok := getScalar("promptVolume", peats.CmdPromptVolumeGet); ok {
		s.PromptVolume = int(v)
	}
	if v, ok := getScalar("voicePack", peats.CmdVoicePackGet); ok {
		s.VoicePack = int(v)
	}

	if f, err := c.Get(peats.CmdCustomKeyGet); err == nil {
		supported["keyMappings"] = true
		s.KeyMappings = f.KeyMappings()
	} else {
		supported["keyMappings"] = false
	}

	s.Supported = supported
	a.mu.Unlock()

	n := 0
	for _, ok := range supported {
		if ok {
			n++
		}
	}
	a.log("log.refreshed", "available", n, "total", len(supported))
	a.pushState()
}

// ------------------------------------------------------------------ writes
//
// Every write is followed by a read back: several `set` commands take effect
// without acknowledging, and resending one would replay the side effect.

func (a *App) settle() { time.Sleep(400 * time.Millisecond) }

func (a *App) SetANCMode(mode int) {
	c := a.conn()
	if c == nil {
		return
	}
	c.Set(peats.CmdANCModeSet, peats.CmdANCModeGet, []byte{byte(mode)})
	a.settle()
	if f, err := c.Get(peats.CmdANCModeGet); err == nil {
		a.mu.Lock()
		a.state.ANCMode = int(f.Scalar())
		a.mu.Unlock()
	}
	a.pushState()
}

func (a *App) SetANCLevel(code int) {
	c := a.conn()
	if c == nil {
		return
	}
	c.Set(peats.CmdANCLevelSet, peats.CmdANCLevelGet, []byte{byte(code)})
	a.settle()
	if f, err := c.Get(peats.CmdANCLevelGet); err == nil {
		a.mu.Lock()
		a.state.ANCLevels = f.Pairs()
		if ac, ok := f.ActiveCode(); ok {
			a.state.ANCLevel = int(ac)
		}
		a.mu.Unlock()
	}
	a.pushState()
}

func (a *App) SetTransparencyLevel(code int) {
	c := a.conn()
	if c == nil {
		return
	}
	c.Set(peats.CmdTransparentLevelSet, peats.CmdTransparentLevelGet, []byte{byte(code)})
	a.settle()
	if f, err := c.Get(peats.CmdTransparentLevelGet); err == nil {
		a.mu.Lock()
		a.state.TransparencyLevels = f.Pairs()
		if ac, ok := f.ActiveCode(); ok {
			a.state.TransparencyLevel = int(ac)
		}
		a.mu.Unlock()
	}
	a.pushState()
}

// setToggle factors the switches out: write, let it settle, read back.
func (a *App) setToggle(setCmd, getCmd peats.Command, on bool, apply func(bool)) {
	c := a.conn()
	if c == nil {
		return
	}
	c.SetBool(setCmd, getCmd, on)
	a.settle()
	if f, err := c.Get(getCmd); err == nil {
		a.mu.Lock()
		apply(f.Scalar() == 1)
		a.mu.Unlock()
	}
	a.pushState()
}

func (a *App) SetGameMode(on bool) {
	a.setToggle(peats.CmdGameModeSet, peats.CmdGameModeGet, on,
		func(v bool) { a.state.GameMode = v })
}

// SetLEAudio toggles LE Audio.
//
// Like LDAC and multipoint, this setting reboots the earbuds: the control
// channel drops immediately. Recovery is handled by handleDisconnect, so the
// read backs that follow are expected to fail.
func (a *App) SetLEAudio(on bool) {
	a.log("log.leAudioToggling")
	a.setToggle(peats.CmdLEAudioSet, peats.CmdLEAudioGet, on,
		func(v bool) { a.state.LEAudio = v })
}

func (a *App) SetTouchDisabled(on bool) {
	a.setToggle(peats.CmdTouchDisableSet, peats.CmdTouchDisableGet, on,
		func(v bool) { a.state.TouchDisabled = v })
}

func (a *App) SetVoicePack(code int) {
	c := a.conn()
	if c == nil {
		return
	}
	c.Set(peats.CmdVoicePackSet, peats.CmdVoicePackGet, []byte{byte(code)})
	a.settle()
	if f, err := c.Get(peats.CmdVoicePackGet); err == nil {
		a.mu.Lock()
		a.state.VoicePack = int(f.Scalar())
		a.mu.Unlock()
	}
	a.pushState()
}

func (a *App) SetPromptVolume(v int) {
	c := a.conn()
	if c == nil {
		return
	}
	c.Set(peats.CmdPromptVolumeSet, peats.CmdPromptVolumeGet, []byte{byte(v)})
	a.settle()
	if f, err := c.Get(peats.CmdPromptVolumeGet); err == nil {
		a.mu.Lock()
		a.state.PromptVolume = int(f.Scalar())
		a.mu.Unlock()
	}
	a.pushState()
}

// SetMultipoint toggles multipoint.
//
// The firmware turns LDAC off by itself when multipoint comes on: there is not
// enough bandwidth for LDAC over two links. So both are read back. The
// operation reconfigures the links and can make the channel briefly
// unavailable.
func (a *App) SetMultipoint(on bool) {
	c := a.conn()
	if c == nil {
		return
	}
	if on {
		a.log("log.multipointEnabling")
	}
	a.log("log.willReboot")
	c.SetBool(peats.CmdMultipointSet, peats.CmdMultipointGet, on)
	time.Sleep(1200 * time.Millisecond) // link reconfiguration

	if f, err := c.Get(peats.CmdMultipointGet); err == nil {
		a.mu.Lock()
		a.state.Multipoint = f.Scalar() == 1
		a.mu.Unlock()
	}
	if f, err := c.Get(peats.CmdLDACGet); err == nil {
		a.mu.Lock()
		a.state.LDAC = f.Scalar() == 1
		a.mu.Unlock()
	}
	a.pushState()
}

// SetLDAC toggles LDAC, which is exclusive with multipoint (see SetMultipoint).
func (a *App) SetLDAC(on bool) {
	c := a.conn()
	if c == nil {
		return
	}
	if on {
		a.log("log.ldacEnabling")
	}
	a.log("log.willReboot")
	c.SetBool(peats.CmdLDACSet, peats.CmdLDACGet, on)
	time.Sleep(1200 * time.Millisecond)

	if f, err := c.Get(peats.CmdLDACGet); err == nil {
		a.mu.Lock()
		a.state.LDAC = f.Scalar() == 1
		a.mu.Unlock()
	}
	if f, err := c.Get(peats.CmdMultipointGet); err == nil {
		a.mu.Lock()
		a.state.Multipoint = f.Scalar() == 1
		a.mu.Unlock()
	}
	a.pushState()
}

// ------------------------------------------------------------------- touch

// SetKeyMappings rewrites the touch mapping.
//
// ⚠ UNVERIFIED on the wire. The payload layout mirrors the one observed on
// reads, but no write has ever been validated. The UI says so.
func (a *App) SetKeyMappings(ms []peats.KeyMapping) {
	c := a.conn()
	if c == nil {
		return
	}
	a.log("log.touchMappingWrite")
	c.Set(peats.CmdCustomKeySet, peats.CmdCustomKeyGet, peats.EncodeKeyMappings(ms))
	a.settle()
	if f, err := c.Get(peats.CmdCustomKeyGet); err == nil {
		a.mu.Lock()
		a.state.KeyMappings = f.KeyMappings()
		a.mu.Unlock()
	}
	a.pushState()
}

// The touch demo mode (CUSTOMIZE_TOUCH_DEBUG / SET_CUSTOMIZE_TOUCH_DEMO) found
// in the Android app was tested on the H3: the command 03 AB 00 00 is properly
// acknowledged (83 AB 00 00 01), but NO frame is emitted on a tap, and taps
// keep performing their normal action. Touch is handled locally by the
// firmware on this model. The feature was dropped rather than exposing a
// button that does nothing.

// --------------------------------------------------------------- equaliser

// ApplyEQ applies a parametric curve.
//
// The firmware decides the EQ protocol version: v1 for Qualcomm chips and a
// hard-coded firmware list, v2/v3 otherwise. The H3 (firmware "S5") uses
// v2/v3. The returned Message is empty on success.
func (a *App) ApplyEQ(bands []peats.EQBand, masterGain float64) Message {
	c := a.conn()
	if c == nil {
		return msg("error.notConnected")
	}
	if err := c.ApplyEQ(bands, masterGain, a.eqUsesV1()); err != nil {
		a.log("log.eqFailed", "error", err.Error())
		return msg("error.raw", "error", err.Error())
	}
	a.log("log.eqApplied", "bands", len(bands), "gain", fmt.Sprintf("%.1f", masterGain))
	return Message{}
}

// eqUsesV1 mirrors needInParallel() from the Android app.
func (a *App) eqUsesV1() bool {
	a.mu.RLock()
	fw := a.state.Firmware
	a.mu.RUnlock()

	for _, code := range []string{
		"CAPSULE3_PROV2", "CAPSULE3_PRO", "WINGS2", "SPACE_PRO",
		"AIR5_LLITE", "PearClipPro_Ldac", "S15", "S22",
	} {
		if strings.HasPrefix(fw, code) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------- advanced

// SendRaw emits an arbitrary frame. Reserved for the advanced panel.
// Never retried: an unknown command may carry a side effect.
func (a *App) SendRaw(cmdHex, payloadHex string, eqVendor bool) Message {
	c := a.conn()
	if c == nil {
		return msg("error.notConnected")
	}
	cb, err := hex.DecodeString(strings.ReplaceAll(cmdHex, " ", ""))
	if err != nil || len(cb) != 2 {
		return msg("error.invalidCommand")
	}
	var pb []byte
	if strings.TrimSpace(payloadHex) != "" {
		pb, err = hex.DecodeString(strings.ReplaceAll(payloadHex, " ", ""))
		if err != nil {
			return msg("error.invalidPayload")
		}
	}
	cmd := peats.Command{cb[0], cb[1]}
	f, ok := c.Set(cmd, cmd, pb)
	if !ok {
		a.log("log.rawNoReply", "cmd", cmd, "payload", strings.ToUpper(payloadHex))
		return Message{}
	}
	a.log("log.rawReply",
		"cmd", cmd,
		"reply", f.Command,
		"payload", strings.ToUpper(hex.EncodeToString(f.Payload)))
	return Message{}
}
