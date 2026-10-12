package peats

import (
	"bytes"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

// fakeConn stands in for an RFCOMM link. Every write is recorded, and respond
// decides which frames the "earbuds" send back. Reads block until a frame
// arrives or the link goes down, so the platform-specific isTimeout is never
// involved.
type fakeConn struct {
	mu      sync.Mutex
	writes  [][]byte
	respond func(req []byte) [][]byte

	incoming chan []byte
	down     chan struct{}
	once     sync.Once
}

func newFakeConn(respond func(req []byte) [][]byte) *fakeConn {
	return &fakeConn{
		respond:  respond,
		incoming: make(chan []byte, 64),
		down:     make(chan struct{}),
	}
}

func (f *fakeConn) Read(p []byte) (int, error) {
	select {
	case b := <-f.incoming:
		return copy(p, b), nil
	case <-f.down:
		return 0, io.EOF
	}
}

func (f *fakeConn) Write(p []byte) (int, error) {
	f.mu.Lock()
	f.writes = append(f.writes, append([]byte(nil), p...))
	respond := f.respond
	f.mu.Unlock()
	if respond != nil {
		for _, r := range respond(p) {
			f.incoming <- r
		}
	}
	return len(p), nil
}

func (f *fakeConn) SetReadTimeout(time.Duration) error { return nil }

func (f *fakeConn) Close() error {
	f.drop()
	return nil
}

// drop takes the link down as a reboot of the earbuds would.
func (f *fakeConn) drop() { f.once.Do(func() { close(f.down) }) }

func (f *fakeConn) written() [][]byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][]byte(nil), f.writes...)
}

// reply builds a frame as the earbuds send it: version 0x03.
func reply(cmd Command, vendor [2]byte, payload ...byte) []byte {
	f := Encode(cmd, payload, vendor)
	f[1] = VersionResponse
	return f
}

// connectFake connects a client to f with timings short enough for tests.
func connectFake(t *testing.T, f *fakeConn, cb Callbacks) *Client {
	t.Helper()
	prevDial, prevSettle, prevTimeout := dialRFCOMM, settleDelay, defaultTimeout
	dialRFCOMM = func([6]byte, uint8, time.Duration) (rfcommConn, error) { return f, nil }
	settleDelay = 20 * time.Millisecond
	defaultTimeout = 150 * time.Millisecond
	t.Cleanup(func() { dialRFCOMM, settleDelay, defaultTimeout = prevDial, prevSettle, prevTimeout })

	c, err := Connect("98:80:BB:40:D4:ED", 11, cb)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(c.Close)
	return c
}

// A write is sent exactly once, acknowledged or not: several `set` commands
// take effect silently, and a resend replays the side effect.
func TestSetIsNeverRetried(t *testing.T) {
	f := newFakeConn(nil)
	c := connectFake(t, f, Callbacks{})

	if _, ok := c.Set(CmdANCModeSet, CmdANCModeGet, []byte{ANCOn}); ok {
		t.Fatal("Set reported an acknowledgement nobody sent")
	}
	w := f.written()
	if len(w) != 1 {
		t.Fatalf("Set wrote %d frames, want exactly 1", len(w))
	}
	if want := Encode(CmdANCModeSet, []byte{ANCOn}, VendorDefault); !bytes.Equal(w[0], want) {
		t.Errorf("wrote % X, want % X", w[0], want)
	}
}

// The acknowledgement of a `set` carries the reply opcode of the `get`: 03 11
// is answered by 83 10. Waiting on 83 11 would hang.
func TestSetAckCarriesTheGetOpcode(t *testing.T) {
	for _, tc := range []struct {
		name   string
		ack    Command
		wantOK bool
	}{
		{"ack on the get opcode", CmdANCModeGet.Response(), true},
		{"echo of the set opcode is not the ack", CmdANCModeSet.Response(), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeConn(func([]byte) [][]byte {
				return [][]byte{reply(tc.ack, VendorDefault, 0x00, ANCOn)}
			})
			c := connectFake(t, f, Callbacks{})
			if _, ok := c.Set(CmdANCModeSet, CmdANCModeGet, []byte{ANCOn}); ok != tc.wantOK {
				t.Errorf("Set acknowledged = %v, want %v", ok, tc.wantOK)
			}
		})
	}
}

// Reads have no side effect and replies are intermittent: a silent `get` is
// tried three times before giving up.
func TestGetRetriesThreeTimes(t *testing.T) {
	f := newFakeConn(nil)
	c := connectFake(t, f, Callbacks{})

	if _, err := c.Get(CmdBatteryLeft); !errors.Is(err, ErrTimeout) {
		t.Fatalf("Get = %v, want ErrTimeout", err)
	}
	if n := len(f.written()); n != readRetries {
		t.Errorf("Get wrote %d frames, want %d", n, readRetries)
	}
}

func TestGetStopsRetryingOnReply(t *testing.T) {
	calls := 0
	f := newFakeConn(func([]byte) [][]byte {
		calls++
		if calls < 2 {
			return nil
		}
		return [][]byte{reply(CmdBatteryLeft.Response(), VendorDefault, 0x00, 0x64)}
	})
	c := connectFake(t, f, Callbacks{})

	got, err := c.Get(CmdBatteryLeft)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Scalar() != 100 {
		t.Errorf("Scalar = %d, want 100", got.Scalar())
	}
	if n := len(f.written()); n != 2 {
		t.Errorf("Get wrote %d frames, want 2 (one silent, one answered)", n)
	}
}

// The equaliser escapes the general conventions: vendor 001D, and 0E 02 is
// answered by 0F 82 rather than 8E 02.
func TestApplyEQUsesItsOwnConventions(t *testing.T) {
	f := newFakeConn(func(req []byte) [][]byte {
		return [][]byte{reply(EQResponse, VendorEQ, 0x01)}
	})
	c := connectFake(t, f, Callbacks{})
	defaultTimeout = 2 * time.Second // long enough that missing the ack shows

	bands := []EQBand{{Freq: 100, Gain: 6, Q: 1}, {Freq: 1000, Gain: -3, Q: 1.4}}
	start := time.Now()
	if err := c.ApplyEQ(bands, 0, false); err != nil {
		t.Fatalf("ApplyEQ: %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("ApplyEQ took %v: the 0F 82 acknowledgement was not recognised", elapsed)
	}

	w := f.written()
	if len(w) != len(bands) {
		t.Fatalf("ApplyEQ wrote %d frames, want one per band (%d)", len(w), len(bands))
	}
	for i, b := range bands {
		want := Encode(CmdEQv2, EncodeEQBand(len(bands), i+1, 0, b, false), VendorEQ)
		if !bytes.Equal(w[i], want) {
			t.Errorf("band %d: wrote % X, want % X", i+1, w[i], want)
		}
	}
}

// OnDisconnect reports a link that dropped on its own, and names the client.
func TestOnDisconnectFiresOnDrop(t *testing.T) {
	gone := make(chan *Client, 1)
	f := newFakeConn(nil)
	c := connectFake(t, f, Callbacks{OnDisconnect: func(cl *Client) { gone <- cl }})

	f.drop()
	select {
	case cl := <-gone:
		if cl != c {
			t.Error("OnDisconnect named another client")
		}
	case <-time.After(time.Second):
		t.Fatal("OnDisconnect never fired after the link dropped")
	}
	if c.Alive() {
		t.Error("a dropped client still reports itself alive")
	}
}

// A deliberate Close is not a link loss: reporting it would trigger an
// automatic reconnection the user just asked to stop.
func TestOnDisconnectSilentOnClose(t *testing.T) {
	gone := make(chan *Client, 1)
	c := connectFake(t, newFakeConn(nil), Callbacks{OnDisconnect: func(cl *Client) { gone <- cl }})

	c.Close()
	select {
	case <-gone:
		t.Fatal("OnDisconnect fired on a deliberate Close")
	case <-time.After(200 * time.Millisecond):
	}
}

// The callbacks are wired before the read loop starts: a notification the
// earbuds send during the settle delay still reaches OnFrame.
func TestOnFrameSeesFramesDuringSettle(t *testing.T) {
	seen := make(chan Frame, 1)
	f := newFakeConn(nil)
	f.incoming <- reply(CmdANCModeGet.Response(), VendorDefault, 0x00, ANCTransparency)

	connectFake(t, f, Callbacks{OnFrame: func(fr Frame) {
		select {
		case seen <- fr:
		default:
		}
	}})

	select {
	case fr := <-seen:
		if fr.Command != CmdANCModeGet.Response() {
			t.Errorf("OnFrame got %s, want %s", fr.Command, CmdANCModeGet.Response())
		}
	case <-time.After(time.Second):
		t.Fatal("a frame received during the settle delay never reached OnFrame")
	}
}

// A link that drops during the settle delay must not be handed back as a
// working client.
func TestConnectFailsWhenTheLinkDropsDuringSettle(t *testing.T) {
	f := newFakeConn(nil)
	f.drop()

	prevDial, prevSettle := dialRFCOMM, settleDelay
	dialRFCOMM = func([6]byte, uint8, time.Duration) (rfcommConn, error) { return f, nil }
	settleDelay = 50 * time.Millisecond
	defer func() { dialRFCOMM, settleDelay = prevDial, prevSettle }()

	if c, err := Connect("98:80:BB:40:D4:ED", 11, Callbacks{}); !errors.Is(err, ErrNotConnected) {
		if c != nil {
			c.Close()
		}
		t.Fatalf("Connect = %v, want ErrNotConnected", err)
	}
}

// Raw returns whatever comes back first — no opcode rule holds for an
// arbitrary command — and refuses a payload its length byte cannot describe.
func TestRaw(t *testing.T) {
	f := newFakeConn(func([]byte) [][]byte {
		return [][]byte{reply(EQResponse, VendorEQ, 0x01)}
	})
	c := connectFake(t, f, Callbacks{})

	got, err := c.Raw(CmdEQv2, []byte{0x01, 0x01}, VendorEQ)
	if err != nil {
		t.Fatalf("Raw: %v", err)
	}
	if got.Command != EQResponse {
		t.Errorf("Raw returned %s, want %s", got.Command, EQResponse)
	}

	if _, err := c.Raw(CmdBatteryLeft, make([]byte, 256), VendorDefault); err == nil {
		t.Error("Raw accepted a 256-byte payload")
	}
	if n := len(f.written()); n != 1 {
		t.Errorf("%d frames written, want 1: the oversized payload must not be sent", n)
	}
}
