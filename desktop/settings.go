package main

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"
)

// settings is what survives a restart: today, the address of the last earbuds
// that connected. Nothing is built in — a shipped default would point every
// user's empty field at one particular pair of earbuds.
type settings struct {
	MAC string `json:"mac,omitempty"`
}

// settingsPath is <user config dir>/OpenPeats/settings.json: ~/.config on
// Linux, %AppData% on Windows.
func settingsPath() (string, error) {
	return settingsPathIn("OpenPeats")
}

// legacySettingsDir is where 0.3.0 kept its settings, when the desktop app
// still carried the name of the manufacturer's own app. Read once, never
// written: the next save lands in the new place.
const legacySettingsDir = "PeatsAudio"

func settingsPathIn(name string) (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name, "settings.json"), nil
}

// loadSettings never fails: a missing or unreadable file means defaults, and
// a first launch must not be an error.
func loadSettings() settings {
	var s settings
	path, err := settingsPath()
	if err != nil {
		return s
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		if legacy, lerr := settingsPathIn(legacySettingsDir); lerr == nil {
			path = legacy
			raw, err = os.ReadFile(legacy)
		}
	}
	if err != nil {
		return s
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		log.Printf("settings: %s ignored: %v", path, err)
		return settings{}
	}
	return s
}

// saveSettings writes through a temporary file, so a crash mid-write leaves
// the previous settings rather than a truncated file.
func saveSettings(s settings) error {
	path, err := settingsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
