package main

import (
	"os/exec"
	"time"

	"openpeats/internal/i18n"
	"openpeats/internal/peats"
)

const batteryPollInterval = 90 * time.Second

// startMonitorLocked starts the battery poller, replacing any running one.
// The caller holds a.mu: starting the poller in the same critical section that
// publishes the client means a Disconnect can never slip in between and leave
// a poller running with nothing to poll.
func (a *App) startMonitorLocked() {
	if a.monitorStop != nil {
		close(a.monitorStop)
	}
	stop := make(chan struct{})
	a.monitorStop = stop

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
		// An alert is only marked as raised when it actually is. Marking it
		// while alerts are off would swallow it for the rest of the
		// discharge once they are turned back on.
		notify := false
		already := a.notified[r.key]
		switch {
		case v <= a.state.BatteryThresh && !already && a.state.BatteryAlert:
			a.notified[r.key] = true
			notify = true
		case v > a.state.BatteryThresh+5 && already:
			a.notified[r.key] = false
		}
		a.mu.Unlock()

		if notify {
			a.notifyLowBattery(r.labelKey, v)
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
		"--app-name=OpenPeats",
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
	v = min(max(v, 5), 50)
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
