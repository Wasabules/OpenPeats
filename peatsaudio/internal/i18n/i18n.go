// Package i18n holds the message catalogue for the strings Go renders itself:
// desktop notifications and the peatsctl command line output.
//
// Strings shown inside the desktop UI are deliberately NOT handled here.
// app.go emits message keys with their arguments, and the frontend renders
// them from its own catalogue (frontend/src/locales). One catalogue per
// renderer means no string is ever translated twice in two places that could
// drift apart.
//
// English is the source language: en.json is the reference, every other
// catalogue is a translation of it and falls back to English key by key.
package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
)

//go:embed locales/*.json
var catalogues embed.FS

// DefaultLocale is the source language. Every other catalogue falls back to it
// for keys it does not define, so a partial translation degrades to English
// rather than to a raw key.
const DefaultLocale = "en"

var (
	mu      sync.RWMutex
	current = DefaultLocale
	loaded  = map[string]map[string]string{}
)

// Available lists the locales shipped in the binary, sorted.
func Available() []string {
	entries, err := catalogues.ReadDir("locales")
	if err != nil {
		return []string{DefaultLocale}
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, strings.TrimSuffix(e.Name(), ".json"))
	}
	sort.Strings(out)
	return out
}

// Normalize maps a BCP 47 tag onto a catalogue we actually ship: "fr-FR" and
// "fr_FR.UTF-8" both become "fr". Unknown tags fall back to DefaultLocale.
func Normalize(tag string) string {
	tag = strings.ToLower(strings.TrimSpace(tag))
	// Strip the encoding and the modifier a POSIX locale carries around.
	if i := strings.IndexAny(tag, ".@"); i >= 0 {
		tag = tag[:i]
	}
	tag = strings.NewReplacer("_", "-").Replace(tag)

	for _, candidate := range []string{tag, primary(tag)} {
		if candidate == "" {
			continue
		}
		for _, avail := range Available() {
			if candidate == avail {
				return avail
			}
		}
	}
	return DefaultLocale
}

func primary(tag string) string {
	if i := strings.Index(tag, "-"); i >= 0 {
		return tag[:i]
	}
	return tag
}

// SetLocale switches the active locale and returns the one actually applied,
// which may differ from the request when no catalogue matches.
func SetLocale(tag string) string {
	applied := Normalize(tag)
	mu.Lock()
	current = applied
	mu.Unlock()
	return applied
}

// Locale returns the active locale.
func Locale() string {
	mu.RLock()
	defer mu.RUnlock()
	return current
}

// DetectLocale reads the environment to guess the user's language. It only
// covers the POSIX variables: on Windows the desktop UI reports the browser
// language through App.SetLanguage, which is a better signal than anything
// the environment offers there.
func DetectLocale() string {
	for _, name := range []string{"LC_ALL", "LC_MESSAGES", "LANG", "LANGUAGE"} {
		if v := os.Getenv(name); v != "" {
			// LANGUAGE holds a colon-separated preference list.
			if i := strings.Index(v, ":"); i >= 0 {
				v = v[:i]
			}
			return Normalize(v)
		}
	}
	return DefaultLocale
}

// T renders the message stored under key in the active locale.
//
// args are name/value pairs substituted into the {name} placeholders of the
// message:
//
//	i18n.T("notify.lowBattery.body", "label", "left earbud", "percent", 18)
//
// A missing key returns the key itself. That is a bug worth spotting, but it
// must never take down a notification or a command line run.
func T(key string, args ...any) string {
	mu.RLock()
	locale := current
	mu.RUnlock()

	msg, ok := lookup(locale, key)
	if !ok && locale != DefaultLocale {
		msg, ok = lookup(DefaultLocale, key)
	}
	if !ok {
		msg = key
	}
	return interpolate(msg, args)
}

func lookup(locale, key string) (string, bool) {
	mu.RLock()
	cat, ready := loaded[locale]
	mu.RUnlock()

	if !ready {
		cat = load(locale)
	}
	msg, ok := cat[key]
	return msg, ok
}

// load reads a catalogue once and memoises it, including the empty result of a
// missing file — an absent catalogue must not be re-read on every lookup.
func load(locale string) map[string]string {
	mu.Lock()
	defer mu.Unlock()
	if cat, ready := loaded[locale]; ready {
		return cat
	}

	cat := map[string]string{}
	if raw, err := catalogues.ReadFile(path.Join("locales", locale+".json")); err == nil {
		// A malformed catalogue leaves cat partially filled; the English
		// fallback covers whatever is missing.
		_ = json.Unmarshal(raw, &cat)
	}
	loaded[locale] = cat
	return cat
}

func interpolate(msg string, args []any) string {
	if len(args) < 2 {
		return msg
	}
	pairs := make([]string, 0, len(args))
	for i := 0; i+1 < len(args); i += 2 {
		name, ok := args[i].(string)
		if !ok {
			continue
		}
		pairs = append(pairs, "{"+name+"}", fmt.Sprint(args[i+1]))
	}
	if len(pairs) == 0 {
		return msg
	}
	return strings.NewReplacer(pairs...).Replace(msg)
}
