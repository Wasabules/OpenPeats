//go:build !linux

package main

import "openpeats/internal/launch"

// newDesktop has nothing to offer outside Linux yet: no panel icon, and no
// single instance — the window is the only way in, as it always was.
func newDesktop(*App, launch.Options) (shell desktopShell, forwarded bool) {
	return noDesktop{}, false
}
