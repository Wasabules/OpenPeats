// Package peats implémente le protocole de contrôle des écouteurs SoundPEATS
// sur RFCOMM/SPP.
//
// Tout ce qui suit est dérivé de PROTOCOL.md et validé sur un SOUNDPEATS H3
// (firmware S5_20250605_V0.4.9). Les constantes marquées « non vérifié » n'ont
// pas été confirmées sur le fil.
package peats

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// En-tête de trame.
//
//	FF | version | flags | len(payload) | vendorId(2) | command(2) | payload
const (
	SOF   byte = 0xFF
	Flags byte = 0x00

	// L'hôte émet en version 0x04 ; les écouteurs répondent en 0x03.
	VersionRequest  byte = 0x04
	VersionResponse byte = 0x03

	HeaderLen = 8
)

// Identifiants vendeur. L'égaliseur utilise un espace distinct.
var (
	VendorDefault = [2]byte{0x00, 0x0A}
	VendorEQ      = [2]byte{0x00, 0x1D}
)

// Command est un opcode sur 2 octets.
type Command [2]byte

func (c Command) String() string { return fmt.Sprintf("%02X%02X", c[0], c[1]) }

// Response renvoie l'opcode de réponse attendu pour une commande de lecture.
// La règle est « bit 7 armé sur le 1er octet » — elle ne vaut PAS pour l'EQ.
func (c Command) Response() Command { return Command{c[0] | 0x80, c[1]} }

// Opcodes. Chaque entrée est notée get / set.
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
	CmdLEAudioSet = Command{0x03, 0x34} // rompt la règle set = get+1

	CmdTouchDisableGet = Command{0x03, 0x12}
	CmdTouchDisableSet = Command{0x03, 0x13}

	CmdCustomKeyGet   = Command{0x03, 0xAA}
	CmdCustomKeySet   = Command{0x03, 0xAB} // écriture NON VÉRIFIÉE
	CmdCustomKeyReset = Command{0x03, 0xAC} // NON VÉRIFIÉ

	CmdPromptVolumeGet = Command{0x03, 0x19}
	CmdPromptVolumeSet = Command{0x03, 0x20}

	CmdVoicePackGet = Command{0x03, 0x21}
	CmdVoicePackSet = Command{0x03, 0x22}

	CmdClassicConn = Command{0x03, 0x17}

	CmdEQv1 = Command{0x0E, 0x01}
	CmdEQv2 = Command{0x0E, 0x02}

	// Destructifs — jamais émis par l'UI sans confirmation explicite.
	CmdReset  = Command{0x03, 0x05}
	CmdReboot = Command{0x03, 0x16}
)

// EQResponse est l'opcode de réponse de la famille EQ.
// Confirmé sur le fil : 0E 02 → 0F 82. La convention diffère du reste
// du protocole (ce n'est PAS 8E 02).
var EQResponse = Command{0x0F, 0x82}

// Modes ANC (payload de CmdANCModeSet).
const (
	ANCOff          byte = 0x00 // normal, ANC arrêté
	ANCOn           byte = 0x01
	ANCTransparency byte = 0x02
	ANCAdaptive     byte = 0x03
	ANCWeak         byte = 0x04
	ANCUndefined    byte = 0xFF
)

// Sous-modes ANC (payload de CmdANCLevelSet).
// Les 4 valeurs 0x11..0x14 correspondent aux 4 entrées de l'app officielle.
const (
	ANCLevelAdaptive byte = 0x11
	ANCLevelIndoor   byte = 0x12
	ANCLevelOutdoor  byte = 0x13
	ANCLevelTraffic  byte = 0x14
)

// Niveaux de transparence.
const (
	TransparencyVoice    byte = 0x01
	TransparencyStandard byte = 0x02
)

// Packs vocaux.
const (
	VoiceEN byte = 0x00
	VoiceZH byte = 0x01
	VoiceDE byte = 0x02
	VoiceJA byte = 0x03
	VoiceFR byte = 0x04
	VoiceES byte = 0x05
	VoiceIT byte = 0x06
)

// Gestes tactiles.
const (
	GestureClickLeft       byte = 0x01
	GestureClickRight      byte = 0x02
	GestureDoubleLeft      byte = 0x03
	GestureDoubleRight     byte = 0x04
	GestureTripleLeft      byte = 0x05
	GestureTripleRight     byte = 0x06
	GestureLongPressLeft   byte = 0x07
	GestureLongPressRight  byte = 0x08
)

// Actions assignables à un geste.
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

// Frame est une trame du protocole.
type Frame struct {
	Version byte
	Flags   byte
	Vendor  [2]byte
	Command Command
	Payload []byte
}

// Encode sérialise une trame prête à être émise.
func Encode(cmd Command, payload []byte, vendor [2]byte) []byte {
	out := make([]byte, 0, HeaderLen+len(payload))
	out = append(out, SOF, VersionRequest, Flags, byte(len(payload)))
	out = append(out, vendor[0], vendor[1])
	out = append(out, cmd[0], cmd[1])
	return append(out, payload...)
}

var errShort = errors.New("trame incomplète")

// Decode extrait la première trame complète de buf.
// Retourne la trame, le nombre d'octets consommés, et une erreur si buf ne
// contient pas encore de trame complète.
func Decode(buf []byte) (Frame, int, error) {
	// Resynchronisation sur le SOF : le flux RFCOMM peut contenir du bruit.
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

// Scalar extrait la valeur utile d'un payload scalaire.
//
// Les réponses de lecture encodent les scalaires sur 2 octets big-endian
// (batterie 00 64 = 100). La valeur tient dans le dernier octet.
func (f Frame) Scalar() byte {
	if len(f.Payload) == 0 {
		return 0
	}
	return f.Payload[len(f.Payload)-1]
}

// Pair est une entrée d'une réponse en liste.
type Pair struct {
	Code   byte `json:"code"`
	Active bool `json:"active"`
}

// Pairs décode les réponses en liste : un octet de tête, puis des paires
// (code, actif). Vaut pour le sous-mode ANC, le niveau de transparence et
// le mapping tactile.
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

// ActiveCode renvoie le code marqué actif dans une réponse en liste.
func (f Frame) ActiveCode() (byte, bool) {
	for _, pr := range f.Pairs() {
		if pr.Active {
			return pr.Code, true
		}
	}
	return 0, false
}

// KeyMapping associe un geste à une action.
type KeyMapping struct {
	Gesture byte `json:"gesture"`
	Action  byte `json:"action"`
}

// KeyMappings décode la réponse du mapping tactile (03 AA) : un octet de tête
// puis des paires (geste, action).
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

// EncodeKeyMappings construit le payload de CmdCustomKeySet.
//
// ⚠ NON VÉRIFIÉ sur le fil. La structure reproduit celle observée en lecture
// (octet de tête puis paires) mais l'écriture n'a jamais été testée.
func EncodeKeyMappings(ms []KeyMapping) []byte {
	out := make([]byte, 0, 1+2*len(ms))
	out = append(out, 0x00)
	for _, m := range ms {
		out = append(out, m.Gesture, m.Action)
	}
	return out
}

// EQBand est une bande d'égaliseur paramétrique (filtre peaking).
type EQBand struct {
	Freq int     `json:"freq"` // Hz
	Gain float64 `json:"gain"` // dB
	Q    float64 `json:"q"`
}

// EncodeEQBand construit le payload d'une bande.
//
// Facteurs d'échelle validés sur le fil : freq × 3, gain × 60, q × 4096,
// masterGain × 60 — tous en int16 big-endian.
//
// v1 compacte total et index dans un seul octet ; v2/v3 les met sur deux.
// index est 1-based.
func EncodeEQBand(total, index int, masterGain float64, b EQBand, v1 bool) []byte {
	var head []byte
	if v1 {
		head = []byte{byte((total&0x0F)<<4 | (index & 0x0F))}
	} else {
		head = []byte{byte(total), byte(index)}
	}

	out := make([]byte, 0, len(head)+8)
	out = append(out, head...)
	out = binary.BigEndian.AppendUint16(out, uint16(int16(masterGain*60)))
	out = binary.BigEndian.AppendUint16(out, uint16(int16(b.Freq*3)))
	out = binary.BigEndian.AppendUint16(out, uint16(int16(b.Gain*60)))
	out = binary.BigEndian.AppendUint16(out, uint16(int16(b.Q*4096)))
	return out
}
