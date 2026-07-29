package i18n

import (
	"bytes"
	"encoding/json"
	"path"
	"regexp"
	"sort"
	"testing"
)

func TestNormalizeMapsTagsOntoShippedCatalogues(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"fr", "fr"},
		{"fr-FR", "fr"},
		{"fr_FR.UTF-8", "fr"},
		{"FR-ca", "fr"},
		{"en-GB", "en"},
		{"", "en"},
		{"kl-GL", "en"}, // no catalogue: fall back rather than return a raw key
	} {
		if got := Normalize(tc.in); got != tc.want {
			t.Errorf("Normalize(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestTInterpolatesNamedPlaceholders(t *testing.T) {
	defer SetLocale(DefaultLocale)
	SetLocale("en")

	got := T("notify.lowBattery.body", "label", "left earbud", "percent", 18)
	if want := "left earbud at 18%."; got != want {
		t.Errorf("T = %q, want %q", got, want)
	}
}

// A missing key must surface as the key itself. Returning an empty string
// would silently blank a notification instead of pointing at the bug.
func TestTFallsBackToTheKey(t *testing.T) {
	if got := T("does.not.exist"); got != "does.not.exist" {
		t.Errorf("T = %q, want the key back", got)
	}
}

// A translation that is missing a key must degrade to English, never to a raw
// key: a partial catalogue is a normal state during translation work.
func TestTranslationFallsBackToEnglish(t *testing.T) {
	defer SetLocale(DefaultLocale)
	SetLocale("fr")

	if got := T("does.not.exist.either"); got != "does.not.exist.either" {
		t.Errorf("T = %q, want the key back", got)
	}
	if got := T("battery.left"); got != "écouteur gauche" {
		t.Errorf("T = %q, want the French message", got)
	}
}

// --- Catalogue consistency ---
//
// English is the source language. Every translation must define exactly the
// same keys with exactly the same placeholders, otherwise a translated build
// shows a raw key or drops an interpolated value.

func TestCataloguesDefineTheSameKeys(t *testing.T) {
	en := readCatalogue(t, DefaultLocale)

	for _, locale := range Available() {
		if locale == DefaultLocale {
			continue
		}
		other := readCatalogue(t, locale)

		for _, key := range sortedKeys(en) {
			if _, ok := other[key]; !ok {
				t.Errorf("%s.json: missing key %q", locale, key)
			}
		}
		for _, key := range sortedKeys(other) {
			if _, ok := en[key]; !ok {
				t.Errorf("%s.json: key %q absent from the English source", locale, key)
			}
		}
	}
}

func TestCataloguesUseTheSamePlaceholders(t *testing.T) {
	en := readCatalogue(t, DefaultLocale)

	for _, locale := range Available() {
		if locale == DefaultLocale {
			continue
		}
		other := readCatalogue(t, locale)

		for _, key := range sortedKeys(en) {
			msg, ok := other[key]
			if !ok {
				continue // already reported by TestCataloguesDefineTheSameKeys
			}
			want, got := placeholders(en[key]), placeholders(msg)
			if len(want) != len(got) {
				t.Errorf("%s.json: %q has placeholders %v, English has %v",
					locale, key, got, want)
				continue
			}
			for i := range want {
				if want[i] != got[i] {
					t.Errorf("%s.json: %q has placeholders %v, English has %v",
						locale, key, got, want)
					break
				}
			}
		}
	}
}

func TestCataloguesAreNotEmpty(t *testing.T) {
	if len(readCatalogue(t, DefaultLocale)) == 0 {
		t.Fatal("the English catalogue is empty: the embed pattern is not matching")
	}
}

// Sorted keys are the only thing keeping a catalogue reviewable once it passes
// a hundred entries — and the only way a diff stays readable when a key moves.
func TestCatalogueKeysAreSorted(t *testing.T) {
	for _, locale := range Available() {
		keys := catalogueKeysInOrder(t, locale)
		for i := 1; i < len(keys); i++ {
			if keys[i-1] > keys[i] {
				t.Errorf("%s.json: keys are not sorted (%q before %q)",
					locale, keys[i-1], keys[i])
				break
			}
		}
	}
}

// catalogueKeysInOrder reads the keys in file order. Unmarshalling into a map
// would lose it, which is exactly what this test is about.
func catalogueKeysInOrder(t *testing.T, locale string) []string {
	t.Helper()
	raw, err := catalogues.ReadFile(path.Join("locales", locale+".json"))
	if err != nil {
		t.Fatalf("reading %s.json: %v", locale, err)
	}

	dec := json.NewDecoder(bytes.NewReader(raw))
	if _, err := dec.Token(); err != nil { // opening brace
		t.Fatalf("%s.json: %v", locale, err)
	}

	var keys []string
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			t.Fatalf("%s.json: %v", locale, err)
		}
		key, ok := tok.(string)
		if !ok {
			t.Fatalf("%s.json: expected a string key, got %T", locale, tok)
		}
		keys = append(keys, key)

		var discard json.RawMessage
		if err := dec.Decode(&discard); err != nil {
			t.Fatalf("%s.json: %v", locale, err)
		}
	}
	return keys
}

var placeholderRe = regexp.MustCompile(`\{[a-zA-Z0-9_]+\}`)

func placeholders(msg string) []string {
	found := placeholderRe.FindAllString(msg, -1)
	sort.Strings(found)
	return found
}

func readCatalogue(t *testing.T, locale string) map[string]string {
	t.Helper()
	raw, err := catalogues.ReadFile(path.Join("locales", locale+".json"))
	if err != nil {
		t.Fatalf("reading %s.json: %v", locale, err)
	}
	var cat map[string]string
	if err := json.Unmarshal(raw, &cat); err != nil {
		t.Fatalf("%s.json is not valid JSON: %v", locale, err)
	}
	return cat
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
