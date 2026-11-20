//go:build linux

package main

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/godbus/dbus/v5"

	"openpeats/internal/freedesktop"
	"openpeats/internal/i18n"
)

// Installing the desktop integration from the application: the launcher entry
// with its actions, its icon, the D-Bus activation file and the GNOME Shell
// extension, for this user only. A package puts the same files under
// /usr/share; this puts them under ~/.local/share, pointing at the binary
// actually running, so a build run from anywhere integrates the same way.
//
// The files are the ones the package ships, embedded rather than copied: the
// launcher entry and the service file in build/linux, the extension in
// gnome-extension.

//go:embed build/linux/openpeats.desktop build/linux/io.github.wasabules.OpenPeats.service build/appicon.svg gnome-extension
var integrationFiles embed.FS

// integrator does the work, with nothing but the session bus: the window's
// card and --install-integration share it.
type integrator struct {
	bus *dbus.Conn
}

// integrationStatus is where each piece stands.
type integrationStatus struct {
	launcher  freedesktop.PieceState
	extension freedesktop.PieceState
	shell     freedesktop.ShellExtensionStatus
}

var execOpenPeats = regexp.MustCompile(`(?m)^Exec=openpeats\b`)

// pieces builds the files for the binary running now.
func (in integrator) pieces() (launcher, extension freedesktop.Piece, err error) {
	exe, err := os.Executable()
	if err != nil {
		return launcher, extension, err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}

	read := func(name string) []byte {
		b, readErr := integrationFiles.ReadFile(name)
		if readErr != nil {
			err = errors.Join(err, readErr)
		}
		return b
	}
	entry := execOpenPeats.ReplaceAll(read("build/linux/openpeats.desktop"),
		[]byte("Exec="+freedesktop.DesktopExecArg(exe)))
	service := strings.ReplaceAll(string(read("build/linux/io.github.wasabules.OpenPeats.service")),
		"Exec=/usr/bin/openpeats", "Exec="+freedesktop.ServiceExecArg(exe))
	launcher = freedesktop.Piece{
		Marker: "applications/openpeats.desktop",
		Files: map[string][]byte{
			"applications/openpeats.desktop":                      entry,
			"dbus-1/services/" + freedesktop.BusName + ".service": []byte(service),
			"icons/hicolor/scalable/apps/openpeats.svg":           read("build/appicon.svg"),
		},
	}

	dir := "gnome-shell/extensions/" + extensionUUID
	extension = freedesktop.Piece{
		Marker: dir + "/metadata.json",
		Dir:    dir,
		Files:  map[string][]byte{},
	}
	walkErr := fs.WalkDir(integrationFiles, "gnome-extension", func(p string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		extension.Files[path.Join(dir, strings.TrimPrefix(p, "gnome-extension/"))] = read(p)
		return nil
	})
	return launcher, extension, errors.Join(err, walkErr)
}

func (in integrator) status() (integrationStatus, error) {
	dirs, err := freedesktop.UserDataDirs()
	if err != nil {
		return integrationStatus{}, err
	}
	launcher, extension, err := in.pieces()
	if err != nil {
		return integrationStatus{}, err
	}
	return integrationStatus{
		launcher:  dirs.State(launcher),
		extension: dirs.State(extension),
		shell:     freedesktop.ShellExtension(in.bus, extensionUUID),
	}, nil
}

// needsInstall: absent, or put there by another build.
func needsInstall(s freedesktop.PieceState) bool {
	return s == freedesktop.PieceMissing || s == freedesktop.PieceOutdated
}

// install puts in place what is missing or out of date, and switches the
// extension on. The extension only goes where GNOME Shell runs.
func (in integrator) install() error {
	dirs, err := freedesktop.UserDataDirs()
	if err != nil {
		return err
	}
	launcher, extension, err := in.pieces()
	if err != nil {
		return err
	}

	if needsInstall(dirs.State(launcher)) {
		if err := dirs.Install(launcher); err != nil {
			return err
		}
		// The activation file works at once, not from the next login.
		_ = freedesktop.ReloadBusConfig(in.bus)
	}

	shell := freedesktop.ShellExtension(in.bus, extensionUUID)
	if !shell.Shell {
		return nil
	}
	if needsInstall(dirs.State(extension)) {
		if err := dirs.Install(extension); err != nil {
			return err
		}
	}
	if !shell.Enabled && !shell.Active {
		if _, err := freedesktop.EnableShellExtension(in.bus, extensionUUID); err != nil {
			return err
		}
	}
	return nil
}

// remove takes down what install put in place: the user's copies, and the
// extension's place in the list. The package's files stay.
func (in integrator) remove() error {
	dirs, err := freedesktop.UserDataDirs()
	if err != nil {
		return err
	}
	launcher, extension, err := in.pieces()
	if err != nil {
		return err
	}
	var errs []error
	if freedesktop.ShellExtension(in.bus, extensionUUID).Shell {
		errs = append(errs, freedesktop.DisableShellExtension(in.bus, extensionUUID))
	}
	if st := dirs.State(extension); st == freedesktop.PieceInstalled || st == freedesktop.PieceOutdated {
		errs = append(errs, dirs.Remove(extension))
	}
	if st := dirs.State(launcher); st == freedesktop.PieceInstalled || st == freedesktop.PieceOutdated {
		errs = append(errs, dirs.Remove(launcher))
		_ = freedesktop.ReloadBusConfig(in.bus)
	}
	return errors.Join(errs...)
}

// view is the status for the window.
func (s integrationStatus) view() Integration {
	return Integration{
		Available:              true,
		Launcher:               string(s.launcher),
		GNOME:                  s.shell.Shell,
		Extension:              string(s.extension),
		ExtensionEnabled:       s.shell.Enabled || s.shell.Active,
		ExtensionActive:        s.shell.Active,
		UserExtensionsDisabled: s.shell.UserExtensionsDisabled,
	}
}

// report turns a status, and the error of the action that led to it, into
// what the window shows.
func (in integrator) report(actionErr error) Integration {
	st, err := in.status()
	if err != nil {
		return Integration{Error: msg("error.integration", "error", err.Error())}
	}
	v := st.view()
	if actionErr != nil {
		v.Error = msg("error.integration", "error", actionErr.Error())
	}
	return v
}

func (d *linuxDesktop) integrator() integrator { return integrator{bus: d.bus} }

func (d *linuxDesktop) integration() Integration { return d.integrator().report(nil) }

func (d *linuxDesktop) installIntegration() Integration {
	in := d.integrator()
	return in.report(in.install())
}

func (d *linuxDesktop) removeIntegration() Integration {
	in := d.integrator()
	return in.report(in.remove())
}

// integrationCommand runs --install-integration or --remove-integration and
// returns the exit code. It needs no running instance and starts none.
func integrationCommand(action string) int {
	bus, err := dbus.ConnectSessionBus()
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.T("integration.failed", "error", err.Error()))
		return 1
	}
	defer bus.Close()
	in := integrator{bus: bus}

	if action == "remove" {
		err = in.remove()
	} else {
		err = in.install()
	}
	st, statusErr := in.status()
	if err = errors.Join(err, statusErr); err != nil {
		fmt.Fprintln(os.Stderr, i18n.T("integration.failed", "error", err.Error()))
	}
	if statusErr != nil {
		return 1
	}

	fmt.Println(i18n.T("integration.launcher", "state", i18n.T("integration.state."+string(st.launcher))))
	switch {
	case !st.shell.Shell:
		fmt.Println(i18n.T("integration.notGNOME"))
	default:
		state := i18n.T("integration.state." + string(st.extension))
		switch {
		case st.extension == freedesktop.PieceMissing:
		case st.shell.Active:
			state += " — " + i18n.T("integration.extension.active")
		case st.shell.Enabled:
			state += " — " + i18n.T("integration.extension.nextLogin")
		default:
			state += " — " + i18n.T("integration.extension.off")
		}
		fmt.Println(i18n.T("integration.extension", "state", state))
		if st.shell.UserExtensionsDisabled {
			fmt.Println(i18n.T("integration.userExtensionsDisabled"))
		}
	}
	if err != nil {
		return 1
	}
	return 0
}
