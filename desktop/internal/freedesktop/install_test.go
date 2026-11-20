package freedesktop

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func testPiece() Piece {
	return Piece{
		Marker: "gnome-shell/extensions/x@y/metadata.json",
		Dir:    "gnome-shell/extensions/x@y",
		Files: map[string][]byte{
			"gnome-shell/extensions/x@y/metadata.json":   []byte(`{"uuid":"x@y"}`),
			"gnome-shell/extensions/x@y/extension.js":    []byte("export default class {}"),
			"gnome-shell/extensions/x@y/locales/fr.json": []byte("{}"),
		},
	}
}

func TestPieceLifecycle(t *testing.T) {
	d := DataDirs{Home: t.TempDir(), System: []string{t.TempDir()}}
	p := testPiece()

	if got := d.State(p); got != PieceMissing {
		t.Fatalf("fresh: %s", got)
	}
	if err := d.Install(p); err != nil {
		t.Fatal(err)
	}
	if got := d.State(p); got != PieceInstalled {
		t.Fatalf("after install: %s", got)
	}

	// Another build: one file changed, one dropped. Installing again leaves
	// no stray file behind in the directory the piece owns.
	stray := filepath.Join(d.Home, "gnome-shell/extensions/x@y/locales/fr.json")
	newer := testPiece()
	newer.Files["gnome-shell/extensions/x@y/extension.js"] = []byte("export default class { v2 }")
	delete(newer.Files, "gnome-shell/extensions/x@y/locales/fr.json")
	if got := d.State(newer); got != PieceOutdated {
		t.Fatalf("other build: %s", got)
	}
	if err := d.Install(newer); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stray); !os.IsNotExist(err) {
		t.Fatalf("dropped file still there: %v", err)
	}
	if got := d.State(newer); got != PieceInstalled {
		t.Fatalf("after update: %s", got)
	}

	if err := d.Remove(newer); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(d.Home, newer.Dir)); !os.IsNotExist(err) {
		t.Fatal("the directory survived the removal")
	}
}

func TestSystemPieceIsLeftAlone(t *testing.T) {
	d := DataDirs{Home: t.TempDir(), System: []string{t.TempDir(), t.TempDir()}}
	p := Piece{
		Marker: "applications/openpeats.desktop",
		Files: map[string][]byte{
			"applications/openpeats.desktop":                        []byte("[Desktop Entry]\n"),
			"dbus-1/services/io.github.wasabules.OpenPeats.service": []byte("[D-BUS Service]\n"),
		},
	}
	sys := filepath.Join(d.System[1], "applications/openpeats.desktop")
	if err := writeAtomic(sys, []byte("[Desktop Entry]\nExec=/usr/bin/openpeats\n")); err != nil {
		t.Fatal(err)
	}
	if got := d.State(p); got != PieceSystem {
		t.Fatalf("got %s, want system", got)
	}
	// A user copy shadows the system one, so it is the one reported.
	if err := d.Install(p); err != nil {
		t.Fatal(err)
	}
	if got := d.State(p); got != PieceInstalled {
		t.Fatalf("got %s with a user copy", got)
	}
	if err := d.Remove(p); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sys); err != nil {
		t.Fatalf("removing the user copy touched the system one: %v", err)
	}
	if err := d.Remove(p); err != nil {
		t.Fatalf("removing twice: %v", err)
	}
}

func TestUserDataDirs(t *testing.T) {
	t.Setenv("HOME", "/home/someone")
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_DATA_DIRS", "")
	d, err := UserDataDirs()
	if err != nil {
		t.Fatal(err)
	}
	if d.Home != "/home/someone/.local/share" || !slices.Equal(d.System, []string{"/usr/local/share", "/usr/share"}) {
		t.Fatalf("defaults: %+v", d)
	}
	// Relative entries are invalid by the specification and ignored.
	t.Setenv("XDG_DATA_HOME", "relative")
	t.Setenv("XDG_DATA_DIRS", "/opt/share:rel:/usr/share")
	d, _ = UserDataDirs()
	if d.Home != "/home/someone/.local/share" || !slices.Equal(d.System, []string{"/opt/share", "/usr/share"}) {
		t.Fatalf("from the environment: %+v", d)
	}
}

func TestExecArgQuoting(t *testing.T) {
	tests := []struct{ path, desktop, service string }{
		{"/usr/bin/openpeats", "/usr/bin/openpeats", "/usr/bin/openpeats"},
		{"/home/a b/openpeats", `"/home/a b/openpeats"`, `'/home/a b/openpeats'`},
		{`/x/it's$`, `"/x/it's\\$"`, `'/x/it'\''s$'`},
	}
	for _, tt := range tests {
		if got := DesktopExecArg(tt.path); got != tt.desktop {
			t.Errorf("DesktopExecArg(%q) = %s, want %s", tt.path, got, tt.desktop)
		}
		if got := ServiceExecArg(tt.path); got != tt.service {
			t.Errorf("ServiceExecArg(%q) = %s, want %s", tt.path, got, tt.service)
		}
	}
}

func TestStrvRoundTrip(t *testing.T) {
	if got := parseStrv("@as []\n"); len(got) != 0 {
		t.Fatalf("empty list parsed as %q", got)
	}
	in := "['ubuntu-dock@ubuntu.com', 'tiling-assistant@ubuntu.com']\n"
	list := parseStrv(in)
	if !slices.Equal(list, []string{"ubuntu-dock@ubuntu.com", "tiling-assistant@ubuntu.com"}) {
		t.Fatalf("parsed %q", list)
	}
	list = append(list, "openpeats@wasabules.github.io", `odd'one\x`)
	if got := parseStrv(formatStrv(list)); !slices.Equal(got, list) {
		t.Fatalf("round trip: %q", got)
	}
	if formatStrv(nil) != "@as []" {
		t.Fatal("an empty list needs its type spelled out")
	}
}
