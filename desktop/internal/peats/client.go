package peats

import (
	"errors"
	"fmt"
	"log"
	"sync"
	"time"
)

// Timings measured against a SOUNDPEATS H3. Variables rather than constants
// only so the tests can shorten them; nothing else assigns them.
var (
	// A command sent too soon after the RFCOMM link comes up gets no reply.
	// ~600 ms is enough.
	settleDelay = 700 * time.Millisecond

	defaultTimeout = 1500 * time.Millisecond
	readTimeout    = 300 * time.Millisecond
)

const readRetries = 3

// Callbacks receive what the read loop sees. They are handed to Connect rather
// than assigned on the returned client: the loop starts before Connect returns,
// and a field written after that point is a data race — and a link that drops
// during the settle delay would find no handler at all.
type Callbacks struct {
	// OnFrame receives every frame, including the spontaneous notifications
	// the earbuds emit. Called from the read loop: it must not block.
	OnFrame func(Frame)

	// OnDisconnect fires when the link drops without being asked to —
	// typically because the earbuds rebooted, which LDAC, multipoint and
	// LE Audio all trigger. Not called on a deliberate Close(). It receives
	// the client that dropped, so a caller juggling a reconnection can tell
	// a stale client from the current one.
	OnDisconnect func(*Client)
}

// Client is a control connection to a pair of earbuds.
//
// The transport comes from the platform layer (transport_*.go); everything
// below is OS independent.
type Client struct {
	mu      sync.Mutex
	conn    rfcommConn
	channel uint8
	closed  bool

	// lost marks a link that dropped on its own. Distinct from closed: the
	// socket still has to be released, but the client is unusable already.
	lost bool

	// exMu serialises exchanges: one in flight at a time.
	exMu sync.Mutex

	frames  chan Frame
	stopped chan struct{}

	cb Callbacks
}

// Connect opens a control connection. channel = 0 triggers discovery.
func Connect(mac string, channel uint8, cb Callbacks) (*Client, error) {
	addr, err := ParseMAC(mac)
	if err != nil {
		return nil, err
	}
	if dialRFCOMM == nil {
		return nil, ErrUnsupported
	}

	var conn rfcommConn
	if channel == 0 {
		conn, channel, err = dialAny(addr)
	} else {
		conn, err = dialRFCOMM(addr, channel, 10*time.Second)
	}
	if err != nil {
		return nil, err
	}

	if err := conn.SetReadTimeout(readTimeout); err != nil {
		log.Printf("peats: read timeout not applied: %v", err)
	}

	c := &Client{
		conn:    conn,
		channel: channel,
		frames:  make(chan Frame, 32),
		stopped: make(chan struct{}),
		cb:      cb,
	}
	go c.readLoop()

	// Let the link settle before the first command.
	time.Sleep(settleDelay)
	if !c.Alive() {
		c.Close()
		return nil, ErrNotConnected
	}
	return c, nil
}

func (c *Client) Channel() uint8 { return c.channel }

// Alive reports whether the client can still carry exchanges: neither closed
// nor dropped. A caller installing a fresh client checks it under its own lock,
// which closes the window where the link drops between Connect returning and
// the client being published.
func (c *Client) Alive() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.closed && !c.lost
}

func (c *Client) transport() (rfcommConn, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.lost || c.conn == nil {
		return nil, ErrNotConnected
	}
	return c.conn, nil
}

func (c *Client) readLoop() {
	// On the way out the client is permanently unusable: mark the loss before
	// closing frames, so in-flight exchanges fail with ErrNotConnected instead
	// of waiting out a full timeout.
	defer func() {
		c.mu.Lock()
		voluntary := c.closed
		c.lost = true
		c.mu.Unlock()

		close(c.frames)

		if !voluntary && c.cb.OnDisconnect != nil {
			c.cb.OnDisconnect(c)
		}
	}()

	buf := make([]byte, 1024)
	var acc []byte

	for {
		select {
		case <-c.stopped:
			return
		default:
		}

		conn, err := c.transport()
		if err != nil {
			return
		}

		n, err := conn.Read(buf)
		if err != nil {
			if isTimeout(err) {
				continue
			}
			return
		}
		if n == 0 {
			return
		}

		acc = append(acc, buf[:n]...)
		for {
			f, used, derr := Decode(acc)
			if derr != nil {
				if used > 0 {
					acc = acc[used:] // noise before the SOF, drop it
				}
				break
			}
			acc = acc[used:]
			if c.cb.OnFrame != nil {
				c.cb.OnFrame(f)
			}
			select {
			case c.frames <- f:
			default: // consumer lagging: never block the read loop
			}
		}
	}
}

// drain empties pending frames before a new exchange, so a stale notification
// is not mistaken for the reply we are waiting on.
//
// ⚠ The `ok` test is not decorative. Once `frames` is closed — which happens
// as soon as the link drops — a receive on it always succeeds immediately, so
// `default` is never selected again. Without the test the loop spins forever
// holding exMu, and freezes the whole application.
func (c *Client) drain() {
	for {
		select {
		case _, ok := <-c.frames:
			if !ok {
				return
			}
		default:
			return
		}
	}
}

func (c *Client) write(b []byte) error {
	conn, err := c.transport()
	if err != nil {
		return err
	}
	_, err = conn.Write(b)
	return err
}

// await waits for a frame satisfying match, bounded by timeout.
func (c *Client) await(match func(Frame) bool, timeout time.Duration) (Frame, error) {
	deadline := time.After(timeout)
	for {
		select {
		case f, ok := <-c.frames:
			if !ok {
				return Frame{}, ErrNotConnected
			}
			if match(f) {
				return f, nil
			}
		case <-deadline:
			return Frame{}, ErrTimeout
		}
	}
}

// Get sends a read command and waits for its reply.
// Reads are retried: they have no side effects.
func (c *Client) Get(cmd Command) (Frame, error) {
	c.exMu.Lock()
	defer c.exMu.Unlock()

	want := cmd.Response()
	var lastErr error

	for try := 0; try < readRetries; try++ {
		c.drain()
		if err := c.write(Encode(cmd, nil, VendorDefault)); err != nil {
			return Frame{}, err
		}
		f, err := c.await(func(f Frame) bool { return f.Command == want }, defaultTimeout)
		if err == nil {
			return f, nil
		}
		lastErr = err
	}
	return Frame{}, fmt.Errorf("%s: %w", cmd, lastErr)
}

// Set sends a write command.
//
// ⚠ Never retried, ever. Several `set` commands take effect *without*
// acknowledging (ANC mode, multipoint); resending one replays the side effect
// — observed on the H3 as three voice prompts for a single intended write.
//
// The acknowledgement, when it comes, generally carries the reply opcode of
// the matching `get` rather than that of the `set`. Its absence is NOT an
// error: the caller must read the state back to verify.
func (c *Client) Set(setCmd, getCmd Command, payload []byte) (Frame, bool) {
	c.exMu.Lock()
	defer c.exMu.Unlock()

	c.drain()
	if err := c.write(Encode(setCmd, payload, VendorDefault)); err != nil {
		log.Printf("peats: write failed %s: %v", setCmd, err)
		return Frame{}, false
	}
	want := getCmd.Response()
	f, err := c.await(func(f Frame) bool { return f.Command == want }, defaultTimeout)
	return f, err == nil
}

// ApplyEQ sends an equaliser curve, one frame per band.
//
// The EQ family uses VendorEQ and answers with EQResponse (0F 82), not with
// the "bit 7" convention the rest of the protocol follows.
func (c *Client) ApplyEQ(bands []EQBand, masterGain float64, v1 bool) error {
	if len(bands) == 0 {
		return errors.New("no band to apply")
	}
	c.exMu.Lock()
	defer c.exMu.Unlock()

	cmd := CmdEQv2
	if v1 {
		cmd = CmdEQv1
	}

	for i, b := range bands {
		c.drain()
		payload := EncodeEQBand(len(bands), i+1, masterGain, b, v1)
		if err := c.write(Encode(cmd, payload, VendorEQ)); err != nil {
			return fmt.Errorf("band %d: %w", i+1, err)
		}
		// The ack is brief; wait for it without making it a failure condition.
		if _, err := c.await(func(f Frame) bool { return f.Command == EQResponse },
			defaultTimeout); err != nil {
			log.Printf("peats: band %d not acknowledged (continuing)", i+1)
		}
		time.Sleep(80 * time.Millisecond)
	}
	return nil
}

// Raw sends an arbitrary frame once and returns the first frame that comes
// back within the timeout, whatever its opcode. Reserved for exploration: a
// raw `set` acknowledges on its get opcode and an EQ frame on 0F 82, so no
// opcode rule can be assumed. Never retried — an unknown command may carry a
// side effect.
func (c *Client) Raw(cmd Command, payload []byte, vendor [2]byte) (Frame, error) {
	if len(payload) > 0xFF {
		return Frame{}, fmt.Errorf("payload of %d bytes: the length field holds 255 at most", len(payload))
	}
	c.exMu.Lock()
	defer c.exMu.Unlock()

	c.drain()
	if err := c.write(Encode(cmd, payload, vendor)); err != nil {
		return Frame{}, err
	}
	return c.await(func(Frame) bool { return true }, defaultTimeout)
}

// Close shuts the control connection down.
func (c *Client) Close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	conn := c.conn
	c.conn = nil
	c.mu.Unlock()

	close(c.stopped)
	if conn != nil {
		conn.Close()
	}
}
