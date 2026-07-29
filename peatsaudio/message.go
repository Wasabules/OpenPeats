package main

import "fmt"

// Message is a localisable string: a catalogue key plus the values to
// interpolate into it.
//
// The Go side never renders UI text. It names what happened and hands the
// arguments over; the frontend owns the UI catalogue
// (frontend/src/locales/*.json) and renders the result in the user's
// language. That way a string exists exactly once, in one catalogue, instead
// of being translated on both sides of the Wails boundary where the two copies
// would drift apart.
//
// Strings Go renders itself — desktop notifications, peatsctl output — go
// through internal/i18n instead, which is a separate catalogue with no overlap
// beyond the few battery labels both sides have to name.
type Message struct {
	Key  string            `json:"key"`
	Args map[string]string `json:"args,omitempty"`
}

// Empty reports whether m carries nothing to display. Methods returning a
// Message use the zero value to mean success.
func (m Message) Empty() bool { return m.Key == "" }

// msg builds a Message from a key and name/value pairs:
//
//	msg("log.connected", "channel", 11)
func msg(key string, args ...any) Message {
	m := Message{Key: key}
	if len(args) < 2 {
		return m
	}
	m.Args = make(map[string]string, len(args)/2)
	for i := 0; i+1 < len(args); i += 2 {
		name, ok := args[i].(string)
		if !ok {
			continue
		}
		m.Args[name] = fmt.Sprint(args[i+1])
	}
	return m
}

// ref marks an argument value as another catalogue key rather than literal
// text, so the frontend translates it before interpolating:
//
//	msg("log.reconnectAttempt", "reason", ref("error.busy"))
//
// This is the linked-message convention i18n libraries use, and it is what
// lets a nested reason ("Reconnect 2/10: control channel busy") stay fully
// translated without Go ever touching the UI catalogue.
func ref(key string) string { return "@:" + key }
