package freedesktop

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Installing the integration for one user, from the application itself: the
// files a package would put under /usr/share go under ~/.local/share instead,
// which every freedesktop desktop reads first. Nothing here needs root, and
// nothing outside the user's data directory is ever written or removed.

// Piece is a set of files that go in together, as paths under a data
// directory.
type Piece struct {
	Files map[string][]byte
	// Marker is the file whose presence tells the piece is there, from a
	// system package or an earlier install.
	Marker string
	// Dir, when set, is a directory the piece owns whole: an install
	// replaces it, so a file the new version dropped does not linger, and a
	// removal deletes it.
	Dir string
}

// PieceState is where a piece stands.
type PieceState string

const (
	// PieceMissing: nowhere to be found.
	PieceMissing PieceState = "missing"
	// PieceSystem: installed by the system's package; nothing to do.
	PieceSystem PieceState = "system"
	// PieceInstalled: in the user's data directory, as this build has it.
	PieceInstalled PieceState = "installed"
	// PieceOutdated: in the user's data directory, from another build —
	// another version, or a binary since moved.
	PieceOutdated PieceState = "outdated"
)

// DataDirs are the XDG data directories.
type DataDirs struct {
	// Home is where the user's own files go: $XDG_DATA_HOME.
	Home string
	// System is where packages put theirs: $XDG_DATA_DIRS.
	System []string
}

// UserDataDirs reads the XDG data directories from the environment, with the
// specification's defaults.
func UserDataDirs() (DataDirs, error) {
	home := os.Getenv("XDG_DATA_HOME")
	if !filepath.IsAbs(home) {
		dir, err := os.UserHomeDir()
		if err != nil {
			return DataDirs{}, err
		}
		home = filepath.Join(dir, ".local", "share")
	}
	var system []string
	for _, dir := range filepath.SplitList(os.Getenv("XDG_DATA_DIRS")) {
		if filepath.IsAbs(dir) {
			system = append(system, dir)
		}
	}
	if len(system) == 0 {
		system = []string{"/usr/local/share", "/usr/share"}
	}
	return DataDirs{Home: home, System: system}, nil
}

// State tells where a piece stands. The user's copy comes first, since it
// is the one the desktop reads.
func (d DataDirs) State(p Piece) PieceState {
	if exists(filepath.Join(d.Home, p.Marker)) {
		for name, want := range p.Files {
			got, err := os.ReadFile(filepath.Join(d.Home, name))
			if err != nil || !bytes.Equal(got, want) {
				return PieceOutdated
			}
		}
		return PieceInstalled
	}
	for _, dir := range d.System {
		if exists(filepath.Join(dir, p.Marker)) {
			return PieceSystem
		}
	}
	return PieceMissing
}

// Install writes the piece into the user's data directory. Each file goes
// through a temporary one, so a reader never sees half of it.
func (d DataDirs) Install(p Piece) error {
	if p.Dir != "" {
		if err := os.RemoveAll(filepath.Join(d.Home, p.Dir)); err != nil {
			return err
		}
	}
	names := make([]string, 0, len(p.Files))
	for name := range p.Files {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if err := writeAtomic(filepath.Join(d.Home, name), p.Files[name]); err != nil {
			return err
		}
	}
	return nil
}

// Remove deletes the user's copy of the piece. A system copy stays: it is not
// ours to remove.
func (d DataDirs) Remove(p Piece) error {
	if p.Dir != "" {
		return os.RemoveAll(filepath.Join(d.Home, p.Dir))
	}
	var errs []error
	for name := range p.Files {
		err := os.Remove(filepath.Join(d.Home, name))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func writeAtomic(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, content, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// DesktopExecArg quotes a program path for the Exec key of a desktop entry,
// following the Desktop Entry Specification: quoted only when it has to be,
// then escaped once more for the key file's own string rules.
func DesktopExecArg(path string) string {
	if !strings.ContainsAny(path, " \t\n\"'\\><~|&;$*?#()`") {
		return path
	}
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range path {
		if strings.ContainsRune("\"`$\\", r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	b.WriteByte('"')
	return strings.ReplaceAll(b.String(), `\`, `\\`)
}

// ServiceExecArg quotes a program path for the Exec key of a D-Bus service
// file, which the bus splits the way a shell would.
func ServiceExecArg(path string) string {
	if !strings.ContainsAny(path, " \t\n\"'\\><~|&;$*?#()`") {
		return path
	}
	return "'" + strings.ReplaceAll(path, "'", `'\''`) + "'"
}
