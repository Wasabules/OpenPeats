package main

import (
	"errors"
	"log"
	"strings"
	"time"

	"openpeats/internal/peats"
)

// callbacks wires a new client to the application. Handed to peats.Connect so
// they are in place before the first frame can arrive.
func (a *App) callbacks() peats.Callbacks {
	return peats.Callbacks{
		// Every frame is mirrored into the UI log.
		OnFrame: a.emitFrame,
		// Several settings reboot the earbuds: without this, the link
		// dropping would go unnoticed and the interface would freeze on a
		// dead state.
		OnDisconnect: a.handleDisconnect,
	}
}

// Connect opens the control connection and does a first refresh.
// The returned Message is empty on success.
func (a *App) Connect(mac string) Message {
	mac = strings.TrimSpace(mac)
	if mac == "" {
		return msg("error.noAddress")
	}
	if _, err := peats.ParseMAC(mac); err != nil {
		return msg("error.invalidAddress")
	}

	a.sessionMu.Lock()

	// Whatever was running stands down: the current client, and any
	// reconnection loop in flight, which checks wantConnected and session.
	a.mu.Lock()
	old := a.client
	a.client = nil
	a.state.Connected = false
	a.wantConnected = false
	a.session++
	a.mu.Unlock()
	if old != nil {
		old.Close()
	}
	a.stopMonitor()

	a.log("log.connecting", "mac", mac)
	cl, err := peats.Connect(mac, 0, a.callbacks())
	if err == nil && !a.install(cl, mac) {
		err = peats.ErrNotConnected // dropped between dialing and publishing
	}
	a.sessionMu.Unlock()

	if err != nil {
		a.log("log.connectFailed")
		a.pushState()
		return describeConnectError(err)
	}

	if err := saveSettings(settings{MAC: mac}); err != nil {
		log.Printf("settings: address not remembered: %v", err)
	}
	a.log("log.connected", "channel", cl.Channel())
	a.Refresh()
	return Message{}
}

// install publishes a freshly dialed client as the current one. It refuses a
// client that already dropped: checking under the same lock handleDisconnect
// takes means a drop is seen either here or there, never by neither.
func (a *App) install(cl *peats.Client, mac string) bool {
	a.mu.Lock()
	if !cl.Alive() {
		a.mu.Unlock()
		cl.Close()
		return false
	}
	a.client = cl
	a.state.Connected = true
	a.state.MAC = mac
	a.state.Channel = int(cl.Channel())
	a.notified = map[string]bool{}
	a.wantConnected = true
	a.session++
	a.startMonitorLocked()
	a.mu.Unlock()
	return true
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
	case errors.Is(err, peats.ErrNotConnected):
		return msg("error.notConnected")
	default:
		return msg("error.raw", "error", err.Error())
	}
}

func (a *App) Disconnect() {
	a.sessionMu.Lock()
	defer a.sessionMu.Unlock()

	a.stopMonitor()
	a.mu.Lock()
	cl := a.client
	a.client = nil
	a.state.Connected = false
	a.wantConnected = false
	a.session++
	a.mu.Unlock()
	if cl != nil {
		cl.Close()
	}
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
func (a *App) handleDisconnect(dead *peats.Client) {
	a.mu.Lock()
	if a.client != dead {
		// Already replaced, or never published: the current session does
		// not depend on it. Closing it here is all it needs — tearing down
		// whatever client is current instead would kill a healthy link.
		a.mu.Unlock()
		dead.Close()
		return
	}
	a.client = nil
	a.state.Connected = false
	mac := a.state.MAC
	retry := a.wantConnected
	a.session++
	session := a.session
	a.mu.Unlock()

	dead.Close() // the socket still needs releasing even with the link gone
	a.stopMonitor()
	a.pushState()

	if !retry {
		a.log("log.linkLost")
		return
	}
	a.log("log.linkLostReconnecting")
	go a.reconnectLoop(mac, session)
}

// reconnectLoop retries the connection while the earbuds reboot. It belongs to
// one session and gives up the moment another starts.
func (a *App) reconnectLoop(mac string, session int) {
	current := func() bool {
		return a.wantConnected && a.session == session && a.client == nil
	}

	for i := 1; i <= reconnectAttempts; i++ {
		time.Sleep(reconnectDelay)

		a.mu.RLock()
		ok := current()
		a.mu.RUnlock()
		if !ok {
			return
		}

		cl, err := peats.Connect(mac, 0, a.callbacks())
		if err != nil {
			a.log("log.reconnectAttempt",
				"attempt", i,
				"total", reconnectAttempts,
				"reason", ref(describeConnectError(err).Key))
			continue
		}

		// The dial takes seconds: the user may have disconnected or
		// connected by hand in the meantime. Check again before publishing,
		// under the lock, or this client would silently undo their choice.
		a.mu.Lock()
		if !current() {
			a.mu.Unlock()
			cl.Close()
			return
		}
		if !cl.Alive() {
			a.mu.Unlock()
			cl.Close()
			continue
		}
		a.client = cl
		a.state.Connected = true
		a.state.Channel = int(cl.Channel())
		a.session++
		a.startMonitorLocked()
		a.mu.Unlock()

		a.log("log.reconnected", "channel", cl.Channel())
		a.Refresh()
		return
	}
	a.log("log.reconnectGaveUp", "attempts", reconnectAttempts)
}
