package freedesktop

import (
	"os"
	"strings"
	"testing"
	"time"
)

// TestLiveBatteryProvider runs against the real bluetoothd: the fake one in
// bus_test.go cannot tell whether the system bus policy lets a user's process
// register a provider, nor whether BlueZ accepts the level. The earbuds must
// be paired; they need not be connected.
//
//	PEATS_MAC=98:80:BB:40:D4:ED go test ./internal/freedesktop -run TestLive -v
func TestLiveBatteryProvider(t *testing.T) {
	mac := os.Getenv("PEATS_MAC")
	if mac == "" {
		t.Skip("PEATS_MAC unset — BlueZ test skipped")
	}
	p, err := NewBatteryProvider()
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	device, _, _, ok := p.findDevice(mac)
	if !ok {
		t.Fatalf("BlueZ knows no device %s", mac)
	}
	battery := func() (byte, bool) {
		v, err := p.conn.Object("org.bluez", device).GetProperty("org.bluez.Battery1.Percentage")
		if err != nil {
			return 0, false
		}
		pct, ok := v.Value().(byte)
		return pct, ok
	}
	if pct, ok := battery(); ok {
		// The earbuds report their own level, over HFP: the provider
		// must leave it alone rather than compete with it.
		p.Set(mac, 42)
		time.Sleep(time.Second)
		if now, _ := battery(); now != pct {
			t.Fatalf("the level went from %d%% to %d%%: OpenPeats took the place of the earbuds' own", pct, now)
		}
		t.Skipf("%s reports its own level (%d%%): left alone, nothing more to check", device, pct)
	}

	p.Set(mac, 42)
	waitFor(t, "org.bluez.Battery1 at 42%", func() bool { pct, ok := battery(); return ok && pct == 42 })

	src, err := p.conn.Object("org.bluez", device).GetProperty("org.bluez.Battery1.Source")
	if err != nil || !strings.Contains(src.Value().(string), "OpenPeats") {
		t.Logf("Battery1.Source = %v (%v)", src, err)
	}

	p.Set(mac, 41)
	waitFor(t, "the level to follow", func() bool { pct, ok := battery(); return ok && pct == 41 })

	p.Set(mac, -1)
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, ok := battery(); !ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the level stayed after it was withdrawn")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
