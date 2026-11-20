package freedesktop

import (
	"context"
	"errors"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

// Switching a GNOME Shell extension on and off. The shell does it at once for
// an extension it knows; one installed since it started is only discovered at
// the next login on Wayland, so for that one the settings are written
// directly, which is all `gnome-extensions enable` amounts to.

// The shell serves its extension interface under its own name. The bus name
// org.gnome.Shell.Extensions is something else: the Extensions application,
// started on demand — calling it would launch that application, not reach the
// shell.
const (
	shellExtensionsName      = "org.gnome.Shell"
	shellExtensionsPath      = dbus.ObjectPath("/org/gnome/Shell")
	shellExtensionsInterface = "org.gnome.Shell.Extensions"
	shellSchema              = "org.gnome.shell"
)

// ShellExtensionStatus is how GNOME Shell stands with one extension.
type ShellExtensionStatus struct {
	// Shell reports that GNOME Shell runs this session.
	Shell bool
	// Active: the extension runs now.
	Active bool
	// Enabled: it is in the list the shell starts, now or at the next login.
	Enabled bool
	// UserExtensionsDisabled: the shell is set to run no user extension at
	// all, whatever the list says.
	UserExtensionsDisabled bool
}

// ShellExtension reads where an extension stands.
func ShellExtension(conn *dbus.Conn, uuid string) ShellExtensionStatus {
	st := ShellExtensionStatus{Shell: hasOwner(conn, shellExtensionsName)}
	if !st.Shell {
		return st
	}
	st.Active = ShellExtensionActive(conn, uuid)
	if list, err := gsettingsGet("enabled-extensions"); err == nil {
		st.Enabled = slices.Contains(parseStrv(list), uuid)
	}
	if v, err := gsettingsGet("disable-user-extensions"); err == nil {
		st.UserExtensionsDisabled = strings.TrimSpace(v) == "true"
	}
	return st
}

// EnableShellExtension switches the extension on, and reports whether it
// runs already or waits for the next login.
func EnableShellExtension(conn *dbus.Conn, uuid string) (active bool, err error) {
	if !hasOwner(conn, shellExtensionsName) {
		return false, errors.New("GNOME Shell is not running")
	}
	var known bool
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = conn.Object(shellExtensionsName, shellExtensionsPath).CallWithContext(ctx,
		shellExtensionsInterface+".EnableExtension", 0, uuid).Store(&known)
	if err == nil && known {
		return ShellExtensionActive(conn, uuid), nil // the shell wrote the settings itself
	}
	// Not known to the shell yet: list it for the next login.
	if err := gsettingsEdit("enabled-extensions", func(l []string) []string {
		if !slices.Contains(l, uuid) {
			l = append(l, uuid)
		}
		return l
	}); err != nil {
		return false, err
	}
	err = gsettingsEdit("disabled-extensions", func(l []string) []string {
		return slices.DeleteFunc(l, func(s string) bool { return s == uuid })
	})
	return false, err
}

// DisableShellExtension switches the extension off, in the running shell and
// in the settings.
func DisableShellExtension(conn *dbus.Conn, uuid string) error {
	if hasOwner(conn, shellExtensionsName) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = conn.Object(shellExtensionsName, shellExtensionsPath).CallWithContext(ctx,
			shellExtensionsInterface+".DisableExtension", 0, uuid).Err
	}
	return gsettingsEdit("enabled-extensions", func(l []string) []string {
		return slices.DeleteFunc(l, func(s string) bool { return s == uuid })
	})
}

// ReloadBusConfig makes the session bus read its service files again, so a
// D-Bus activation file just written works before the next login.
func ReloadBusConfig(conn *dbus.Conn) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.ReloadConfig", 0).Err
}

func hasOwner(conn *dbus.Conn, name string) bool {
	var owned bool
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.NameHasOwner", 0, name).
		Store(&owned)
	return err == nil && owned
}

func gsettingsGet(key string) (string, error) {
	out, err := exec.Command("gsettings", "get", shellSchema, key).Output()
	return string(out), err
}

// gsettingsEdit reads a string list, changes it and writes it back, only when
// it did change.
func gsettingsEdit(key string, change func([]string) []string) error {
	cur, err := gsettingsGet(key)
	if err != nil {
		return err
	}
	before := parseStrv(cur)
	after := change(slices.Clone(before))
	if slices.Equal(before, after) {
		return nil
	}
	return exec.Command("gsettings", "set", shellSchema, key, formatStrv(after)).Run()
}

var strvItem = regexp.MustCompile(`'((?:[^'\\]|\\.)*)'|"((?:[^"\\]|\\.)*)"`)

// parseStrv reads a GVariant string array as gsettings prints it:
// ['a', 'b'], or @as [] when empty.
func parseStrv(s string) []string {
	var out []string
	for _, m := range strvItem.FindAllStringSubmatch(s, -1) {
		item := m[1]
		if item == "" {
			item = m[2]
		}
		out = append(out, unescapeVariant(item))
	}
	return out
}

func unescapeVariant(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// formatStrv writes a string array in GVariant text format.
func formatStrv(list []string) string {
	if len(list) == 0 {
		return "@as []"
	}
	quoted := make([]string, len(list))
	for i, s := range list {
		s = strings.ReplaceAll(s, `\`, `\\`)
		quoted[i] = "'" + strings.ReplaceAll(s, "'", `\'`) + "'"
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}
