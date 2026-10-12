package peats

import (
	"errors"
	"os"
	"testing"
	"time"
)

// --- Unit tests: no hardware required. ---

// The frame codec, list replies, firmware string and EQ encoding are pinned
// down by the shared vectors (vectors_test.go), which the Kotlin port checks
// too. What follows is specific to the Go side.

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
