// Package peats implements the SoundPEATS earbud control protocol over
// RFCOMM/SPP.
//
// Everything here is derived from PROTOCOL.md and validated against a
// SOUNDPEATS H3 (firmware S5_20250605_V0.4.9). Constants marked "unverified"
// have not been confirmed on the wire.
package peats

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

// Frame header.
//
//	FF | version | flags | len(payload) | vendorId(2) | command(2) | payload
const (
	SOF   byte = 0xFF
	Flags byte = 0x00

	// The host sends version 0x04; the earbuds answer with 0x03.
	VersionRequest  byte = 0x04
	VersionResponse byte = 0x03

	HeaderLen = 8
)

// Vendor identifiers. The equaliser uses a separate space.
var (
	VendorDefault = [2]byte{0x00, 0x0A}
	VendorEQ      = [2]byte{0x00, 0x1D}
)

// Command is a two-byte opcode.
type Command [2]byte

func (c Command) String() string { return fmt.Sprintf("%02X%02X", c[0], c[1]) }

// Response returns the reply opcode expected for a read command. The rule is
// "bit 7 set on the first byte" — it does NOT hold for the equaliser.
func (c Command) Response() Command { return Command{c[0] | 0x80, c[1]} }

// Opcodes. Each entry is annotated get / set.
var (
	CmdBatteryLeft  = Command{0x03, 0x06}
	CmdBatteryRight = Command{0x03, 0x07}
	CmdBatteryCase  = Command{0x03, 0x23}
	CmdFirmware     = Command{0x03, 0x09}

	CmdANCModeGet = Command{0x03, 0x10}
	CmdANCModeSet = Command{0x03, 0x11}

	CmdANCLevelGet = Command{0x03, 0x24}
	CmdANCLevelSet = Command{0x03, 0x25}

	CmdTransparentLevelGet = Command{0x03, 0x4C}
	CmdTransparentLevelSet = Command{0x03, 0x4D}

	CmdGameModeGet = Command{0x03, 0x0E}
	CmdGameModeSet = Command{0x03, 0x0F}

	CmdMultipointGet = Command{0x03, 0x14}
	CmdMultipointSet = Command{0x03, 0x15}

	CmdLDACGet = Command{0x03, 0x38}
	CmdLDACSet = Command{0x03, 0x39}

	CmdLEAudioGet = Command{0x03, 0x35}
	CmdLEAudioSet = Command{0x03, 0x34} // breaks the set = get+1 rule

	CmdTouchDisableGet = Command{0x03, 0x12}
	CmdTouchDisableSet = Command{0x03, 0x13}

	CmdCustomKeyGet   = Command{0x03, 0xAA}
	CmdCustomKeySet   = Command{0x03, 0xAB} // write UNVERIFIED
	CmdCustomKeyReset = Command{0x03, 0xAC} // UNVERIFIED

	CmdPromptVolumeGet = Command{0x03, 0x19}
	CmdPromptVolumeSet = Command{0x03, 0x20}

	CmdVoicePackGet = Command{0x03, 0x21}
	CmdVoicePackSet = Command{0x03, 0x22}

	CmdClassicConn = Command{0x03, 0x17}

	CmdEQv1 = Command{0x0E, 0x01}
	CmdEQv2 = Command{0x0E, 0x02}

	// Destructive — never sent by the UI without explicit confirmation.
	CmdReset  = Command{0x03, 0x05}
	CmdReboot = Command{0x03, 0x16}
)

// EQResponse is the reply opcode of the equaliser family.
// Confirmed on the wire: 0E 02 → 0F 82. The convention differs from the rest
// of the protocol (it is NOT 8E 02).
var EQResponse = Command{0x0F, 0x82}

// ANC modes (payload of CmdANCModeSet).
const (
	ANCOff          byte = 0x00 // normal, ANC stopped
	ANCOn           byte = 0x01
	ANCTransparency byte = 0x02
	ANCAdaptive     byte = 0x03
	ANCWeak         byte = 0x04
	ANCUndefined    byte = 0xFF
)

// ANC sub-modes (payload of CmdANCLevelSet).
// The four values 0x11..0x14 match the four entries of the official app.
const (
	ANCLevelAdaptive byte = 0x11
	ANCLevelIndoor   byte = 0x12
	ANCLevelOutdoor  byte = 0x13
	ANCLevelTraffic  byte = 0x14
)

// Transparency levels.
const (
	TransparencyVoice    byte = 0x01
	TransparencyStandard byte = 0x02
)

// Voice prompt packs.
const (
	VoiceEN byte = 0x00
	VoiceZH byte = 0x01
	VoiceDE byte = 0x02
	VoiceJA byte = 0x03
	VoiceFR byte = 0x04
	VoiceES byte = 0x05
	VoiceIT byte = 0x06
)

// Touch gestures.
const (
	GestureClickLeft      byte = 0x01
	GestureClickRight     byte = 0x02
	GestureDoubleLeft     byte = 0x03
	GestureDoubleRight    byte = 0x04
	GestureTripleLeft     byte = 0x05
	GestureTripleRight    byte = 0x06
	GestureLongPressLeft  byte = 0x07
	GestureLongPressRight byte = 0x08
)

// Actions assignable to a gesture.
const (
	ActionVolumeUp   byte = 0x01
	ActionVolumeDown byte = 0x02
	ActionPlayPause  byte = 0x03
	ActionGameMode   byte = 0x04
	ActionPrevTrack  byte = 0x05
	ActionNextTrack  byte = 0x06
	ActionANCMode    byte = 0x07
	ActionAssistant  byte = 0x08
)

// Frame is a protocol frame.
type Frame struct {
	Version byte
	Flags   byte
	Vendor  [2]byte
	Command Command
	Payload []byte
}

// Encode serialises a frame ready to be sent.
func Encode(cmd Command, payload []byte, vendor [2]byte) []byte {
	out := make([]byte, 0, HeaderLen+len(payload))
	out = append(out, SOF, VersionRequest, Flags, byte(len(payload)))
	out = append(out, vendor[0], vendor[1])
	out = append(out, cmd[0], cmd[1])
	return append(out, payload...)
}

var errShort = errors.New("incomplete frame")

// Decode extracts the first complete frame from buf.
// It returns the frame, the number of bytes consumed, and an error when buf
// does not hold a complete frame yet.
func Decode(buf []byte) (Frame, int, error) {
	// Resynchronise on the SOF: the RFCOMM stream can carry noise.
	off := 0
	for off < len(buf) && buf[off] != SOF {
		off++
	}
	buf = buf[off:]

	if len(buf) < HeaderLen {
		return Frame{}, off, errShort
	}
	total := HeaderLen + int(buf[3])
	if len(buf) < total {
		return Frame{}, off, errShort
	}

	f := Frame{
		Version: buf[1],
		Flags:   buf[2],
		Vendor:  [2]byte{buf[4], buf[5]},
		Command: Command{buf[6], buf[7]},
		Payload: append([]byte(nil), buf[HeaderLen:total]...),
	}
	return f, off + total, nil
}

// Scalar extracts the useful value out of a scalar payload.
//
// Read replies encode scalars as two big-endian bytes (battery 00 64 = 100).
// The value fits in the last byte.
func (f Frame) Scalar() byte {
	if len(f.Payload) == 0 {
		return 0
	}
	return f.Payload[len(f.Payload)-1]
}

// Firmware decodes the version reply (03 09): one status byte, then ASCII.
// Trailing NUL and space padding is dropped.
func (f Frame) Firmware() string {
	if len(f.Payload) <= 1 {
		return ""
	}
	return strings.TrimRight(string(f.Payload[1:]), "\x00 ")
}

// Pair is one entry of a list reply.
type Pair struct {
	Code   byte `json:"code"`
	Active bool `json:"active"`
}

// Pairs decodes list replies: one header byte, then (code, active) pairs.
// This holds for the ANC sub-mode, the transparency level and the touch
// mapping.
func (f Frame) Pairs() []Pair {
	p := f.Payload
	if len(p) < 3 {
		return nil
	}
	out := make([]Pair, 0, (len(p)-1)/2)
	for i := 1; i+1 < len(p); i += 2 {
		out = append(out, Pair{Code: p[i], Active: p[i+1] == 0x01})
	}
	return out
}

// ActiveCode returns the code flagged active in a list reply.
func (f Frame) ActiveCode() (byte, bool) {
	for _, pr := range f.Pairs() {
		if pr.Active {
			return pr.Code, true
		}
	}
	return 0, false
}

// KeyMapping binds a gesture to an action.
type KeyMapping struct {
	Gesture byte `json:"gesture"`
	Action  byte `json:"action"`
}

// KeyMappings decodes the touch mapping reply (03 AA): one header byte, then
// (gesture, action) pairs.
func (f Frame) KeyMappings() []KeyMapping {
	p := f.Payload
	if len(p) < 3 {
		return nil
	}
	out := make([]KeyMapping, 0, (len(p)-1)/2)
	for i := 1; i+1 < len(p); i += 2 {
		out = append(out, KeyMapping{Gesture: p[i], Action: p[i+1]})
	}
	return out
}

// EncodeKeyMappings builds the payload of CmdCustomKeySet.
//
// ⚠ UNVERIFIED on the wire. The layout mirrors the one observed on reads
// (header byte then pairs) but the write has never been tested.
func EncodeKeyMappings(ms []KeyMapping) []byte {
	out := make([]byte, 0, 1+2*len(ms))
	out = append(out, 0x00)
	for _, m := range ms {
		out = append(out, m.Gesture, m.Action)
	}
	return out
}

// EQBand is one band of the parametric equaliser (peaking filter).
type EQBand struct {
	Freq int     `json:"freq"` // Hz
	Gain float64 `json:"gain"` // dB
	Q    float64 `json:"q"`
}

// Equaliser limits. They are the clients' own ranges, not limits read from
// the firmware — see PROTOCOL.md section 7. EncodeEQBand enforces them, so a
// value typed out of range can never wrap around its int16 field on its way to
// the earbuds (q × 4096 overflows from Q = 8).
const (
	EQFreqMin = 20
	EQFreqMax = 20000
	EQGainMin = -12.0
	EQGainMax = 12.0
	EQQMin    = 0.3
	EQQMax    = 6.0
)

// EncodeEQBand builds the payload of a single band.
//
// Scale factors validated on the wire: freq × 3, gain × 60, q × 4096,
// masterGain × 60 — all big-endian, two bytes. Gains are signed; above
// 10922 Hz freq × 3 no longer fits an int16 and goes out as the same two
// bytes read unsigned.
//
// v1 packs total and index into a single byte; v2/v3 spread them over two.
// index is 1-based. Out-of-range values are clamped to the limits above.
func EncodeEQBand(total, index int, masterGain float64, b EQBand, v1 bool) []byte {
	var head []byte
	if v1 {
		head = []byte{byte((total&0x0F)<<4 | (index & 0x0F))}
	} else {
		head = []byte{byte(total), byte(index)}
	}

	masterGain = clamp(masterGain, EQGainMin, EQGainMax)
	freq := min(max(b.Freq, EQFreqMin), EQFreqMax)
	gain := clamp(b.Gain, EQGainMin, EQGainMax)
	q := clamp(b.Q, EQQMin, EQQMax)

	out := make([]byte, 0, len(head)+8)
	out = append(out, head...)
	out = binary.BigEndian.AppendUint16(out, uint16(int16(masterGain*60)))
	out = binary.BigEndian.AppendUint16(out, uint16(freq*3))
	out = binary.BigEndian.AppendUint16(out, uint16(int16(gain*60)))
	out = binary.BigEndian.AppendUint16(out, uint16(int16(q*4096)))
	return out
}

// clamp bounds v to [lo, hi]. NaN, which JSON cannot carry but arithmetic can
// produce, falls back to lo rather than reaching the int16 conversion.
func clamp(v, lo, hi float64) float64 {
	if v != v || v < lo {
		return lo
	}
	return min(v, hi)
}

// eqV1Firmwares lists the firmware families that take the v1 equaliser frame.
// See PROTOCOL.md section 5.
var eqV1Firmwares = []string{
	"CAPSULE3_PROV2", "CAPSULE3_PRO", "WINGS2", "SPACE_PRO",
	"AIR5_LLITE", "PearClipPro_Ldac", "S15", "S22",
}

// EQUsesV1 reports whether a firmware takes the v1 equaliser frame.
//
// ⚠ Inferred, half of the rule only. PROTOCOL.md section 5 also routes every
// Qualcomm chip to v1, but nothing on the SPP link tells the chip vendor
// apart, so only the firmware list is applied, matched as a prefix of the
// version string. Only the H3 (S5, v2/v3) has been checked on hardware.
func EQUsesV1(firmware string) bool {
	for _, code := range eqV1Firmwares {
		if strings.HasPrefix(firmware, code) {
			return true
		}
	}
	return false
}
