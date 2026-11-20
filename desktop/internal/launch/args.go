// Package launch reads the desktop application's command line.
//
// The arguments are few and come from the desktop rather than from a person:
// the autostart entry, D-Bus activation, the launcher's actions. Anything
// else on the line is left alone — `wails dev` adds flags of its own.
package launch

import (
	"errors"
	"strconv"
	"strings"
)

// Options is what the command line asks for.
type Options struct {
	// Background starts without the window and connects to the remembered
	// earbuds. The window shows anyway when nothing in the panel could
	// bring it back.
	Background bool
	// ANCMode is the noise control mode to switch to — 0 off, 1 noise
	// cancelling, 2 transparency — or -1 for none. Like Background, it does
	// not open the window.
	ANCMode int
	// Integration is "install" or "remove" to set the desktop integration
	// up or take it down for this user, then exit; empty otherwise.
	Integration string
}

// ErrInvalidANC reports an --anc value outside 0–2.
var ErrInvalidANC = errors.New("--anc takes 0, 1 or 2")

// Parse reads the arguments, without the program name.
func Parse(args []string) (Options, error) {
	opts := Options{ANCMode: -1}
	for i := 0; i < len(args); i++ {
		name, value, hasValue := strings.Cut(args[i], "=")
		switch name {
		case "--background":
			opts.Background = true
		case "--install-integration":
			opts.Integration = "install"
		case "--remove-integration":
			opts.Integration = "remove"
		case "--anc":
			if !hasValue {
				if i+1 >= len(args) {
					return opts, ErrInvalidANC
				}
				i++
				value = args[i]
			}
			mode, err := strconv.Atoi(value)
			if err != nil || mode < 0 || mode > 2 {
				return opts, ErrInvalidANC
			}
			opts.ANCMode = mode
		}
	}
	return opts, nil
}

// Window reports whether the window should show at start.
func (o Options) Window() bool {
	return !o.Background && o.ANCMode < 0 && o.Integration == ""
}
