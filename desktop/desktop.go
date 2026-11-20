package main

import (
	"errors"
	"slices"

	"openpeats/internal/i18n"
	"openpeats/internal/peats"
)

// desktopShell is what the operating system's desktop offers around the
// window. Linux has a panel icon, a control service for the GNOME Shell
// extension and the launcher, and BlueZ's battery reporting
// (desktop_linux.go); elsewhere there is nothing yet (desktop_other.go).
type desktopShell interface {
	// start runs once Wails is up and the window exists.
	start()
	// stateChanged follows every pushState.
	stateChanged()
	// hasIndicator reports whether something outside the window shows that
	// the application runs, and can bring the window back.
	hasIndicator() bool
	// closedToBackground runs when closing the window only hid it.
	closedToBackground()
	// close runs at shutdown.
	close()

	// integration reports what is installed for this user; install and
	// remove change it, and report the result.
	integration() Integration
	installIntegration() Integration
	removeIntegration() Integration
}

// Integration is how far OpenPeats is wired into the desktop, for the window's
// card and for --install-integration. Only Linux has anything to wire yet.
type Integration struct {
	// Available: this platform and session can be integrated with.
	Available bool `json:"available"`
	// Launcher is the launcher entry with its actions, its icon and the
	// D-Bus activation file: "missing", "installed", "outdated" (by another
	// build) or "system" (by the package).
	Launcher string `json:"launcher"`
	// GNOME: GNOME Shell runs this session, so the extension applies.
	GNOME bool `json:"gnome"`
	// Extension is the Quick Settings extension, in the same terms.
	Extension string `json:"extension"`
	// ExtensionEnabled: listed to run, now or from the next login.
	ExtensionEnabled bool `json:"extensionEnabled"`
	// ExtensionActive: running in the shell right now.
	ExtensionActive bool `json:"extensionActive"`
	// UserExtensionsDisabled: GNOME is set to run no user extension.
	UserExtensionsDisabled bool `json:"userExtensionsDisabled"`
	// Error is empty unless the last install or removal failed.
	Error Message `json:"error"`
}

// noDesktop is the desktop of a platform with no integration.
type noDesktop struct{}

func (noDesktop) start()                          {}
func (noDesktop) stateChanged()                   {}
func (noDesktop) hasIndicator() bool              { return false }
func (noDesktop) closedToBackground()             {}
func (noDesktop) close()                          {}
func (noDesktop) integration() Integration        { return Integration{} }
func (noDesktop) installIntegration() Integration { return Integration{} }
func (noDesktop) removeIntegration() Integration  { return Integration{} }

// DesktopIntegration reports what of the desktop integration is installed.
func (a *App) DesktopIntegration() Integration { return a.desktop.integration() }

// InstallDesktopIntegration installs, updates and switches on what is missing,
// for this user only.
func (a *App) InstallDesktopIntegration() Integration { return a.desktop.installIntegration() }

// RemoveDesktopIntegration takes down what InstallDesktopIntegration put in
// place. What the system's package installed stays.
func (a *App) RemoveDesktopIntegration() Integration { return a.desktop.removeIntegration() }

// ancCycle is the order a one-button control goes through, the same as the
// Android tile's.
var ancCycle = []int{int(peats.ANCOff), int(peats.ANCOn), int(peats.ANCTransparency)}

// nextANCMode is the mode after current. A mode outside the cycle, which
// another model may report, moves to noise cancelling.
func nextANCMode(current int) int {
	i := slices.Index(ancCycle, current)
	if i < 0 {
		return int(peats.ANCOn)
	}
	return ancCycle[(i+1)%len(ancCycle)]
}

// connectErrorText renders a connection failure for a notification: the
// window is not on screen to show it.
func connectErrorText(err error) string {
	switch {
	case errors.Is(err, peats.ErrBusy):
		return i18n.T("err.busy")
	case errors.Is(err, peats.ErrUnreachable):
		return i18n.T("err.unreachable")
	case errors.Is(err, peats.ErrNoChannel):
		return i18n.T("err.noChannel")
	case errors.Is(err, peats.ErrUnsupported):
		return i18n.T("err.unsupported")
	default:
		return err.Error()
	}
}
