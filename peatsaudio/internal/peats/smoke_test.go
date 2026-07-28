package peats

import (
	"encoding/hex"
	"os"
	"strings"
	"testing"
)

// --- Tests unitaires : pas de matériel requis. ---

func TestEncodeFrame(t *testing.T) {
	got := Encode(CmdBatteryLeft, nil, VendorDefault)
	want := "FF040000000A0306"
	if strings.ToUpper(hex.EncodeToString(got)) != want {
		t.Fatalf("Encode = %X, attendu %s", got, want)
	}
}

func TestDecodeBatteryResponse(t *testing.T) {
	// Trame réellement capturée sur le H3.
	raw, _ := hex.DecodeString("FF030002000A83060064")
	f, used, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if used != len(raw) {
		t.Fatalf("consommé %d octets, attendu %d", used, len(raw))
	}
	if f.Version != VersionResponse {
		t.Errorf("version = %02X, attendu %02X", f.Version, VersionResponse)
	}
	if f.Command != (Command{0x83, 0x06}) {
		t.Errorf("commande = %s, attendu 8306", f.Command)
	}
	if got := f.Scalar(); got != 100 {
		t.Errorf("Scalar = %d, attendu 100 (payload 2 octets big-endian)", got)
	}
}

func TestDecodeResyncsOnNoise(t *testing.T) {
	raw, _ := hex.DecodeString("AABBCCFF030002000A83060064")
	f, _, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if f.Command != (Command{0x83, 0x06}) {
		t.Errorf("resynchronisation ratée: %s", f.Command)
	}
}

func TestDecodeIncomplete(t *testing.T) {
	raw, _ := hex.DecodeString("FF030002000A8306")
	if _, _, err := Decode(raw); err == nil {
		t.Fatal("une trame tronquée doit être signalée incomplète")
	}
}

func TestPairsAndActiveCode(t *testing.T) {
	// Réponse réelle du sous-mode ANC : 00 | 1100 | 1201 | 1300 | 1400
	raw, _ := hex.DecodeString("FF030009000A8324001100120113001400")
	f, _, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	pairs := f.Pairs()
	if len(pairs) != 4 {
		t.Fatalf("%d paires, attendu 4", len(pairs))
	}
	code, ok := f.ActiveCode()
	if !ok || code != ANCLevelIndoor {
		t.Errorf("code actif = %02X (ok=%v), attendu %02X", code, ok, ANCLevelIndoor)
	}
}

func TestKeyMappings(t *testing.T) {
	// Mapping réel lu sur le H3.
	raw, _ := hex.DecodeString("FF030011000A83AA0001020201030304030504060807070806")
	f, _, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	ms := f.KeyMappings()
	if len(ms) != 8 {
		t.Fatalf("%d mappings, attendu 8", len(ms))
	}
	if ms[0].Gesture != GestureClickLeft || ms[0].Action != ActionVolumeDown {
		t.Errorf("1er mapping = %+v, attendu clic gauche → volume −", ms[0])
	}
	if ms[7].Gesture != GestureLongPressRight || ms[7].Action != ActionNextTrack {
		t.Errorf("dernier mapping = %+v, attendu appui long droit → piste suivante", ms[7])
	}
}

func TestEncodeEQBand(t *testing.T) {
	// Trame validée sur le fil : +6 dB à 100 Hz, Q 1.0, gain maître 0.
	got := EncodeEQBand(1, 1, 0, EQBand{Freq: 100, Gain: 6, Q: 1.0}, false)
	want := "01010000012C01681000"
	if strings.ToUpper(hex.EncodeToString(got)) != want {
		t.Fatalf("EncodeEQBand = %X, attendu %s", got, want)
	}
}

func TestEncodeEQBandV1PacksHeader(t *testing.T) {
	got := EncodeEQBand(3, 2, 0, EQBand{Freq: 100, Gain: 0, Q: 1.0}, true)
	if got[0] != 0x32 {
		t.Errorf("en-tête v1 = %02X, attendu 32 (total<<4 | index)", got[0])
	}
}

func TestParseMACIsLittleEndian(t *testing.T) {
	got, err := ParseMAC("98:80:BB:40:D4:ED")
	if err != nil {
		t.Fatal(err)
	}
	want := [6]byte{0xED, 0xD4, 0x40, 0xBB, 0x80, 0x98}
	if got != want {
		t.Errorf("ParseMAC = %X, attendu %X", got, want)
	}
}

// --- Test d'intégration : nécessite les écouteurs allumés. ---
//
//	PEATS_MAC=98:80:BB:40:D4:ED go test ./internal/peats -run TestLive -v

func TestLiveReadState(t *testing.T) {
	mac := os.Getenv("PEATS_MAC")
	if mac == "" {
		t.Skip("PEATS_MAC non défini — test matériel ignoré")
	}

	c, err := Connect(mac, 0)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer c.Close()
	t.Logf("connecté sur le canal RFCOMM %d", c.Channel())

	for _, tc := range []struct {
		name string
		cmd  Command
	}{
		{"batterie gauche", CmdBatteryLeft},
		{"batterie droite", CmdBatteryRight},
		{"batterie étui", CmdBatteryCase},
		{"firmware", CmdFirmware},
		{"mode ANC", CmdANCModeGet},
		{"sous-mode ANC", CmdANCLevelGet},
		{"LDAC", CmdLDACGet},
		{"multipoint", CmdMultipointGet},
		{"tactile", CmdCustomKeyGet},
	} {
		f, err := c.Get(tc.cmd)
		if err != nil {
			t.Errorf("%s (%s): %v", tc.name, tc.cmd, err)
			continue
		}
		t.Logf("%-16s %s → payload %X", tc.name, tc.cmd, f.Payload)
	}
}
