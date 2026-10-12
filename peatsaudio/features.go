package main

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"peatsaudio/internal/peats"
)

// feature is one readable setting: the key the frontend knows it by, the read
// command, and how a reply lands in the state. The same table drives Refresh
// and the read back after every write, so a reply is decoded one way only.
type feature struct {
	key   string
	get   peats.Command
	apply func(s *State, f peats.Frame)
}

// features lists everything Refresh queries, in the order it queries them.
// Adding a feature for another model is one entry here plus its UI.
//
// Each key is rendered by the frontend as feature.<key> in the list of absent
// features; scripts/check-locales.mjs reads the `key:` fields of this table to
// make sure every one of them has a catalogue entry.
var features = []feature{
	{key: "batteryLeft", get: peats.CmdBatteryLeft,
		apply: func(s *State, f peats.Frame) { s.BatteryLeft = int(f.Scalar()) }},
	{key: "batteryRight", get: peats.CmdBatteryRight,
		apply: func(s *State, f peats.Frame) { s.BatteryRight = int(f.Scalar()) }},
	{key: "batteryCase", get: peats.CmdBatteryCase,
		apply: func(s *State, f peats.Frame) { s.BatteryCase = int(f.Scalar()) }},
	{key: "firmware", get: peats.CmdFirmware,
		apply: func(s *State, f peats.Frame) {
			if fw := f.Firmware(); fw != "" {
				s.Firmware = fw
			}
		}},
	{key: "ancMode", get: peats.CmdANCModeGet,
		apply: func(s *State, f peats.Frame) { s.ANCMode = int(f.Scalar()) }},
	{key: "ancLevel", get: peats.CmdANCLevelGet,
		apply: func(s *State, f peats.Frame) {
			s.ANCLevels = f.Pairs()
			if code, ok := f.ActiveCode(); ok {
				s.ANCLevel = int(code)
			}
		}},
	{key: "transparencyLevel", get: peats.CmdTransparentLevelGet,
		apply: func(s *State, f peats.Frame) {
			s.TransparencyLevels = f.Pairs()
			if code, ok := f.ActiveCode(); ok {
				s.TransparencyLevel = int(code)
			}
		}},
	{key: "gameMode", get: peats.CmdGameModeGet,
		apply: func(s *State, f peats.Frame) { s.GameMode = f.Scalar() == 1 }},
	{key: "multipoint", get: peats.CmdMultipointGet,
		apply: func(s *State, f peats.Frame) { s.Multipoint = f.Scalar() == 1 }},
	{key: "ldac", get: peats.CmdLDACGet,
		apply: func(s *State, f peats.Frame) { s.LDAC = f.Scalar() == 1 }},
	{key: "leAudio", get: peats.CmdLEAudioGet,
		apply: func(s *State, f peats.Frame) { s.LEAudio = f.Scalar() == 1 }},
	{key: "touchDisabled", get: peats.CmdTouchDisableGet,
		apply: func(s *State, f peats.Frame) { s.TouchDisabled = f.Scalar() == 1 }},
	{key: "promptVolume", get: peats.CmdPromptVolumeGet,
		apply: func(s *State, f peats.Frame) { s.PromptVolume = int(f.Scalar()) }},
	{key: "voicePack", get: peats.CmdVoicePackGet,
		apply: func(s *State, f peats.Frame) { s.VoicePack = int(f.Scalar()) }},
	{key: "keyMappings", get: peats.CmdCustomKeyGet,
		apply: func(s *State, f peats.Frame) { s.KeyMappings = f.KeyMappings() }},
}

// featureByGet finds the table entry for a read command.
func featureByGet(get peats.Command) feature {
	for _, ft := range features {
		if ft.get == get {
			return ft
		}
	}
	panic("no feature reads " + get.String())
}

// ------------------------------------------------------------------- reads

// Refresh reads the whole device state back and updates the support map.
//
// The replies are collected first and merged under the lock once: holding it
// across the reads would stall every other call for as long as the silent
// commands take to time out — 4.5 s each.
func (a *App) Refresh() {
	c := a.conn()
	if c == nil {
		return
	}

	type reply struct {
		ft feature
		f  peats.Frame
	}
	var replies []reply
	supported := map[string]bool{}

	for _, ft := range features {
		f, err := c.Get(ft.get)
		switch {
		case err == nil:
			supported[ft.key] = true
			replies = append(replies, reply{ft, f})
		case errors.Is(err, peats.ErrTimeout):
			// Silent across every retry: absent from this model.
			supported[ft.key] = false
		default:
			// The link went down, not the feature. Leave the support map
			// as it was rather than dim every card; handleDisconnect
			// reports the loss.
			return
		}
	}

	a.mu.Lock()
	if a.client != c { // replaced while reading: these replies are stale
		a.mu.Unlock()
		return
	}
	for _, r := range replies {
		r.ft.apply(&a.state, r.f)
	}
	a.state.Supported = supported
	a.mu.Unlock()

	a.log("log.refreshed", "available", len(replies), "total", len(supported))
	a.pushState()
}

// ------------------------------------------------------------------ writes

// settleDelay is the pause between a write and its read back.
const settleDelay = 400 * time.Millisecond

// rebootDelay is the pause after a write that reconfigures the links.
const rebootDelay = 1200 * time.Millisecond

// write sends one `set` and reads the given features back.
//
// The write is never resent: several `set` commands take effect without
// acknowledging, and a resend would replay the side effect. The read back is
// what tells whether it landed. The first command in readBack is the `get`
// whose reply opcode acknowledges the write.
func (a *App) write(set peats.Command, payload []byte, settle time.Duration, readBack ...peats.Command) {
	c := a.conn()
	if c == nil {
		return
	}
	c.Set(set, readBack[0], payload)
	time.Sleep(settle)

	for _, get := range readBack {
		ft := featureByGet(get)
		f, err := c.Get(get)
		if err != nil {
			continue
		}
		a.mu.Lock()
		if a.client == c {
			ft.apply(&a.state, f)
		}
		a.mu.Unlock()
	}
	a.pushState()
}

func boolByte(on bool) []byte {
	if on {
		return []byte{1}
	}
	return []byte{0}
}

func (a *App) SetANCMode(mode int) {
	a.write(peats.CmdANCModeSet, []byte{byte(mode)}, settleDelay, peats.CmdANCModeGet)
}

func (a *App) SetANCLevel(code int) {
	a.write(peats.CmdANCLevelSet, []byte{byte(code)}, settleDelay, peats.CmdANCLevelGet)
}

func (a *App) SetTransparencyLevel(code int) {
	a.write(peats.CmdTransparentLevelSet, []byte{byte(code)}, settleDelay,
		peats.CmdTransparentLevelGet)
}

func (a *App) SetGameMode(on bool) {
	a.write(peats.CmdGameModeSet, boolByte(on), settleDelay, peats.CmdGameModeGet)
}

// SetLEAudio toggles LE Audio.
//
// Like LDAC and multipoint, this setting reboots the earbuds: the control
// channel drops immediately. Recovery is handled by handleDisconnect, so the
// read back that follows is expected to fail.
func (a *App) SetLEAudio(on bool) {
	a.log("log.leAudioToggling")
	a.write(peats.CmdLEAudioSet, boolByte(on), settleDelay, peats.CmdLEAudioGet)
}

func (a *App) SetTouchDisabled(on bool) {
	a.write(peats.CmdTouchDisableSet, boolByte(on), settleDelay, peats.CmdTouchDisableGet)
}

func (a *App) SetVoicePack(code int) {
	a.write(peats.CmdVoicePackSet, []byte{byte(code)}, settleDelay, peats.CmdVoicePackGet)
}

// SetPromptVolume sets the voice prompt volume, 0–100.
func (a *App) SetPromptVolume(v int) {
	v = min(max(v, 0), 100)
	a.write(peats.CmdPromptVolumeSet, []byte{byte(v)}, settleDelay, peats.CmdPromptVolumeGet)
}

// SetMultipoint toggles multipoint.
//
// The firmware turns LDAC off by itself when multipoint comes on: there is not
// enough bandwidth for LDAC over two links. So both are read back. The
// operation reconfigures the links and can make the channel briefly
// unavailable.
func (a *App) SetMultipoint(on bool) {
	if on {
		a.log("log.multipointEnabling")
	}
	a.log("log.willReboot")
	a.write(peats.CmdMultipointSet, boolByte(on), rebootDelay,
		peats.CmdMultipointGet, peats.CmdLDACGet)
}

// SetLDAC toggles LDAC, which is exclusive with multipoint (see SetMultipoint).
func (a *App) SetLDAC(on bool) {
	if on {
		a.log("log.ldacEnabling")
	}
	a.log("log.willReboot")
	a.write(peats.CmdLDACSet, boolByte(on), rebootDelay,
		peats.CmdLDACGet, peats.CmdMultipointGet)
}

// ------------------------------------------------------------------- touch

// SetKeyMappings rewrites the touch mapping.
//
// ⚠ UNVERIFIED on the wire. The payload layout mirrors the one observed on
// reads, but no write has ever been validated. The UI says so.
func (a *App) SetKeyMappings(ms []peats.KeyMapping) {
	a.log("log.touchMappingWrite")
	a.write(peats.CmdCustomKeySet, peats.EncodeKeyMappings(ms), settleDelay,
		peats.CmdCustomKeyGet)
}

// The touch demo mode (CUSTOMIZE_TOUCH_DEBUG / SET_CUSTOMIZE_TOUCH_DEMO) found
// in the Android app was tested on the H3: the command 03 AB 00 00 is properly
// acknowledged (83 AB 00 00 01), but NO frame is emitted on a tap, and taps
// keep performing their normal action. Touch is handled locally by the
// firmware on this model. The feature was dropped rather than exposing a
// button that does nothing.

// --------------------------------------------------------------- equaliser

// ApplyEQ applies a parametric curve. The encoder clamps every value to the
// limits in peats (EQFreqMin and friends), so whatever the frontend sends, no
// field can wrap around on its way to the earbuds.
//
// The firmware decides the EQ protocol version (see peats.EQUsesV1): the H3
// (firmware "S5") uses v2/v3. The returned Message is empty on success.
func (a *App) ApplyEQ(bands []peats.EQBand, masterGain float64) Message {
	c := a.conn()
	if c == nil {
		return msg("error.notConnected")
	}
	if err := c.ApplyEQ(bands, masterGain, a.eqUsesV1()); err != nil {
		a.log("log.eqFailed", "error", err.Error())
		return msg("error.raw", "error", err.Error())
	}
	a.log("log.eqApplied", "bands", len(bands), "gain", fmt.Sprintf("%.1f", masterGain))
	return Message{}
}

// eqUsesV1 picks the EQ frame version from the firmware read on refresh.
func (a *App) eqUsesV1() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return peats.EQUsesV1(a.state.Firmware)
}

// ---------------------------------------------------------------- advanced

// SendRaw emits an arbitrary frame. Reserved for the advanced panel.
// Never retried: an unknown command may carry a side effect. eqVendor sends it
// under the equaliser's vendor id (00 1D) instead of the default one.
func (a *App) SendRaw(cmdHex, payloadHex string, eqVendor bool) Message {
	c := a.conn()
	if c == nil {
		return msg("error.notConnected")
	}
	cb, err := hex.DecodeString(strings.ReplaceAll(cmdHex, " ", ""))
	if err != nil || len(cb) != 2 {
		return msg("error.invalidCommand")
	}
	var pb []byte
	if strings.TrimSpace(payloadHex) != "" {
		pb, err = hex.DecodeString(strings.ReplaceAll(payloadHex, " ", ""))
		if err != nil {
			return msg("error.invalidPayload")
		}
	}
	if len(pb) > 0xFF {
		return msg("error.payloadTooLong")
	}

	vendor := peats.VendorDefault
	if eqVendor {
		vendor = peats.VendorEQ
	}
	cmd := peats.Command{cb[0], cb[1]}
	payload := strings.ToUpper(hex.EncodeToString(pb))

	f, err := c.Raw(cmd, pb, vendor)
	switch {
	case errors.Is(err, peats.ErrTimeout):
		a.log("log.rawNoReply", "cmd", cmd, "payload", payload)
		return Message{}
	case err != nil:
		return msg("error.raw", "error", err.Error())
	}
	a.log("log.rawReply",
		"cmd", cmd,
		"reply", f.Command,
		"payload", strings.ToUpper(hex.EncodeToString(f.Payload)))
	return Message{}
}
