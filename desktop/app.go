package main

import (
	"context"
	"encoding/hex"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"openpeats/internal/i18n"
	"openpeats/internal/peats"
)

// The App is split by concern, all in package main so Wails binds one type:
//
//	app.go       state, lifecycle, events, language
//	session.go   paired devices, connect, disconnect, recovery after a reboot
//	features.go  the feature table, refresh, writes, EQ, raw frames
//	battery.go   level polling and low-battery alerts
//	settings.go  what survives a restart
//	desktop*.go  the desktop around the window: panel, quick settings, BlueZ

// State is the full device state, pushed to the frontend.
type State struct {
	Connected bool `json:"connected"`
	// MAC is the address last connected to, remembered across restarts.
	// Empty until the user has connected once: no address is built in.
	MAC      string `json:"mac"`
	Channel  int    `json:"channel"`
	Firmware string `json:"firmware"`

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

	// session is bumped by every connect, disconnect and link loss. A
	// reconnection loop carries the value it started under and stands down
	// as soon as it changes, so two loops never race each other.
	session int

	// sessionMu serialises the user's Connect and Disconnect. A Disconnect
	// clicked while a Connect is still dialing waits for it, instead of
	// being undone by it a few seconds later.
	sessionMu sync.Mutex

	monitorStop chan struct{}

	// desktop is what the system offers around the window — on Linux the
	// panel icon, the control service and the BlueZ battery level. It
	// follows every state change.
	desktop desktopShell

	// quitting tells a real quit apart from the window's close button,
	// which only hides the window when something in the panel can bring it
	// back.
	quitting atomic.Bool
}

func NewApp() *App {
	return &App{
		state: State{
			MAC:           loadSettings().MAC,
			Supported:     map[string]bool{},
			BatteryAlert:  true,
			BatteryThresh: 20,
			Language:      i18n.Locale(),
		},
		notified: map[string]bool{},
		desktop:  noDesktop{},
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.desktop.start()
}

// beforeClose decides what closing the window means. With something in the
// panel to bring it back, the window hides and the link stays up: reaching the
// earbuds from the panel is the point of having it there. Without, closing
// quits, as it always has — an application running with nothing on screen to
// show for it would just look like a leak.
func (a *App) beforeClose(ctx context.Context) (prevent bool) {
	if a.quitting.Load() || !a.desktop.hasIndicator() {
		return false
	}
	runtime.WindowHide(ctx)
	a.desktop.closedToBackground()
	return true
}

// quit leaves for good, whatever the panel offers.
func (a *App) quit() {
	a.quitting.Store(true)
	if a.ctx != nil {
		runtime.Quit(a.ctx)
	}
}

// showWindow brings the window back, from the panel or a second launch.
func (a *App) showWindow() {
	if a.ctx == nil {
		return
	}
	runtime.WindowShow(a.ctx)
	runtime.WindowUnminimise(a.ctx) // raises it, where the compositor lets it
}

func (a *App) shutdown(context.Context) {
	a.desktop.close()
	a.stopMonitor()
	a.mu.Lock()
	cl := a.client
	a.client = nil
	a.wantConnected = false
	a.session++
	a.mu.Unlock()
	if cl != nil {
		cl.Close()
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

// emitFrame broadcasts a frame to the UI console. Called from the read loop.
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
	a.desktop.stateChanged()
}

// GetState returns the current state (initial frontend call).
func (a *App) GetState() State {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.state
}

func (a *App) conn() *peats.Client {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.client
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
