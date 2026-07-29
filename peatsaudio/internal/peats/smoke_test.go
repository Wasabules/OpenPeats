package peats

import (
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// --- Unit tests: no hardware required. ---

func TestEncodeFrame(t *testing.T) {
	got := Encode(CmdBatteryLeft, nil, VendorDefault)
	want := "FF040000000A0306"
	if strings.ToUpper(hex.EncodeToString(got)) != want {
		t.Fatalf("Encode = %X, want %s", got, want)
	}
}

func TestDecodeBatteryResponse(t *testing.T) {
	// Frame actually captured on the H3.
	raw, _ := hex.DecodeString("FF030002000A83060064")
	f, used, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if used != len(raw) {
		t.Fatalf("consumed %d bytes, want %d", used, len(raw))
	}
	if f.Version != VersionResponse {
		t.Errorf("version = %02X, want %02X", f.Version, VersionResponse)
	}
	if f.Command != (Command{0x83, 0x06}) {
		t.Errorf("command = %s, want 8306", f.Command)
	}
	if got := f.Scalar(); got != 100 {
		t.Errorf("Scalar = %d, want 100 (two-byte big-endian payload)", got)
	}
}

func TestDecodeResyncsOnNoise(t *testing.T) {
	raw, _ := hex.DecodeString("AABBCCFF030002000A83060064")
	f, _, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if f.Command != (Command{0x83, 0x06}) {
		t.Errorf("resynchronisation failed: %s", f.Command)
	}
}

func TestDecodeIncomplete(t *testing.T) {
	raw, _ := hex.DecodeString("FF030002000A8306")
	if _, _, err := Decode(raw); err == nil {
		t.Fatal("a truncated frame must be reported as incomplete")
	}
}

func TestPairsAndActiveCode(t *testing.T) {
	// Real ANC sub-mode reply: 00 | 1100 | 1201 | 1300 | 1400
	raw, _ := hex.DecodeString("FF030009000A8324001100120113001400")
	f, _, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	pairs := f.Pairs()
	if len(pairs) != 4 {
		t.Fatalf("%d pairs, want 4", len(pairs))
	}
	code, ok := f.ActiveCode()
	if !ok || code != ANCLevelIndoor {
		t.Errorf("active code = %02X (ok=%v), want %02X", code, ok, ANCLevelIndoor)
	}
}

func TestKeyMappings(t *testing.T) {
	// Real mapping read from the H3.
	raw, _ := hex.DecodeString("FF030011000A83AA0001020201030304030504060807070806")
	f, _, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	ms := f.KeyMappings()
	if len(ms) != 8 {
		t.Fatalf("%d mappings, want 8", len(ms))
	}
	if ms[0].Gesture != GestureClickLeft || ms[0].Action != ActionVolumeDown {
		t.Errorf("first mapping = %+v, want left click → volume down", ms[0])
	}
	if ms[7].Gesture != GestureLongPressRight || ms[7].Action != ActionNextTrack {
		t.Errorf("last mapping = %+v, want right long press → next track", ms[7])
	}
}

func TestEncodeEQBand(t *testing.T) {
	// Frame validated on the wire: +6 dB at 100 Hz, Q 1.0, master gain 0.
	got := EncodeEQBand(1, 1, 0, EQBand{Freq: 100, Gain: 6, Q: 1.0}, false)
	want := "01010000012C01681000"
	if strings.ToUpper(hex.EncodeToString(got)) != want {
		t.Fatalf("EncodeEQBand = %X, want %s", got, want)
	}
}

func TestEncodeEQBandV1PacksHeader(t *testing.T) {
	got := EncodeEQBand(3, 2, 0, EQBand{Freq: 100, Gain: 0, Q: 1.0}, true)
	if got[0] != 0x32 {
		t.Errorf("v1 header = %02X, want 32 (total<<4 | index)", got[0])
	}
}

// ParseMAC keeps the natural MAC order. The reversal sockaddr_rc expects
// happens in the Linux transport, because Windows wants a 64-bit integer in
// reading order instead.
func TestParseMACKeepsNaturalOrder(t *testing.T) {
	got, err := ParseMAC("98:80:BB:40:D4:ED")
	if err != nil {
		t.Fatal(err)
	}
	want := [6]byte{0x98, 0x80, 0xBB, 0x40, 0xD4, 0xED}
	if got != want {
		t.Errorf("ParseMAC = %X, want %X", got, want)
	}
}

func TestParseMACRejectsGarbage(t *testing.T) {
	for _, s := range []string{"", "98:80:BB:40:D4", "zz:80:BB:40:D4:ED"} {
		if _, err := ParseMAC(s); err == nil {
			t.Errorf("ParseMAC(%q) should have failed", s)
		}
	}
}

// Link loss: LDAC, multipoint and LE Audio all reboot the earbuds. readLoop
// then closes `frames`, and a receive on a closed channel always succeeds —
// drain() has to notice instead of spinning forever.
func TestDrainReturnsOnClosedChannel(t *testing.T) {
	c := &Client{frames: make(chan Frame, 4)}
	c.frames <- Frame{Command: CmdBatteryLeft}
	close(c.frames)

	done := make(chan struct{})
	go func() {
		c.drain()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("drain() spins on a closed channel: the application would freeze")
	}
}

// After a link loss, exchanges must fail right away rather than wait out a
// timeout that will never produce anything.
func TestExchangesFailFastAfterLoss(t *testing.T) {
	c := &Client{frames: make(chan Frame), stopped: make(chan struct{})}
	c.lost = true

	done := make(chan error, 1)
	go func() {
		_, err := c.Get(CmdBatteryLeft)
		done <- err
	}()

	select {
	case err := <-done:
		if !errors.Is(err, ErrNotConnected) {
			t.Fatalf("Get after link loss = %v, want ErrNotConnected", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Get stays blocked after the link is lost")
	}
}

// --- Integration test: requires the earbuds to be powered on. ---
//
//	PEATS_MAC=98:80:BB:40:D4:ED go test ./internal/peats -run TestLive -v

func TestLiveReadState(t *testing.T) {
	mac := os.Getenv("PEATS_MAC")
	if mac == "" {
		t.Skip("PEATS_MAC unset — hardware test skipped")
	}

	c, err := Connect(mac, 0)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer c.Close()
	t.Logf("connected on RFCOMM channel %d", c.Channel())

	for _, tc := range []struct {
		name string
		cmd  Command
	}{
		{"left battery", CmdBatteryLeft},
		{"right battery", CmdBatteryRight},
		{"case battery", CmdBatteryCase},
		{"firmware", CmdFirmware},
		{"ANC mode", CmdANCModeGet},
		{"ANC sub-mode", CmdANCLevelGet},
		{"LDAC", CmdLDACGet},
		{"multipoint", CmdMultipointGet},
		{"touch mapping", CmdCustomKeyGet},
	} {
		f, err := c.Get(tc.cmd)
		if err != nil {
			t.Errorf("%s (%s): %v", tc.name, tc.cmd, err)
			continue
		}
		t.Logf("%-16s %s → payload %X", tc.name, tc.cmd, f.Payload)
	}
}
