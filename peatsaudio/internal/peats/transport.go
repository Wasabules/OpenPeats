package peats

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// The PeatsAudio protocol runs over RFCOMM (Bluetooth Classic, SPP profile).
// Every OS exposes that transport through a different API; this layer is the
// only part that changes from one platform to the next.
//
//	Linux   AF_BLUETOOTH / BTPROTO_RFCOMM   (transport_linux.go)
//	Windows AF_BTH / BTHPROTO_RFCOMM        (transport_windows.go)
//	macOS   IOBluetooth                     (not written yet: transport_stub.go)
//
// iOS is out of reach: SPP there is reserved for MFi-certified accessories,
// which these earbuds are not.

// rfcommConn is an established RFCOMM connection.
type rfcommConn interface {
	Read(p []byte) (int, error)
	Write(p []byte) (int, error)
	// SetReadTimeout bounds how long a Read waits. Expiry must return an
	// error satisfying isTimeout.
	SetReadTimeout(d time.Duration) error
	Close() error
}

// dialRFCOMM opens a connection to addr on the given channel.
// Implemented once per platform, in its own file.
//
// It must return an error satisfying errors.Is(err, ErrBusy) when the channel
// is already held by another device, and errors.Is(err, ErrRefused) when no
// service listens on that channel.
var dialRFCOMM func(addr [6]byte, channel uint8, timeout time.Duration) (rfcommConn, error)

// maxChannel bounds the discovery sweep. RFCOMM channels run from 1 to 30.
const maxChannel = 30

// maxConsecutiveTimeouts ends the sweep early. A reachable device answers a
// closed channel at once with a refusal; only an absent one lets every attempt
// run into the dial timeout, which would otherwise cost 30 × 3 s before saying
// so. Three rather than one leaves room for a slow first page.
const maxConsecutiveTimeouts = 3

var (
	// ErrBusy: the control channel is monopolised by another device.
	// Typically multipoint enabled with a phone connected.
	ErrBusy = errors.New("control channel held by another device")

	// ErrRefused: no service listens on that channel.
	ErrRefused = errors.New("channel refused")

	// ErrUnreachable: the device is paired but unreachable — off, in its
	// case, or out of range. Distinct from ErrRefused: the channel is not
	// closed, the device simply is not there. No channel will answer, so
	// sweeping is pointless.
	ErrUnreachable = errors.New("earbuds unreachable")

	// ErrNoChannel: no RFCOMM channel is open.
	ErrNoChannel = errors.New("no RFCOMM channel available")

	ErrTimeout      = errors.New("no reply from the earbuds")
	ErrNotConnected = errors.New("not connected")

	// ErrUnsupported: platform with no RFCOMM transport implemented.
	ErrUnsupported = errors.New("RFCOMM transport unavailable on this platform")
)

// ParseMAC turns "98:80:BB:40:D4:ED" into a byte array, most significant byte
// first.
//
// Careful: each platform API expects a different order. Linux wants it
// reversed (little-endian), Windows wants a 64-bit integer. That conversion
// belongs to each transport, not here.
func ParseMAC(s string) ([6]byte, error) {
	var out [6]byte
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) != 6 {
		return out, fmt.Errorf("invalid MAC address: %q", s)
	}
	for i, p := range parts {
		v, err := strconv.ParseUint(p, 16, 8)
		if err != nil {
			return out, fmt.Errorf("invalid MAC byte %q: %w", p, err)
		}
		out[i] = byte(v)
	}
	return out, nil
}

// dialAny sweeps the RFCOMM channels and returns the FIRST connection it
// establishes.
//
// sdptool returns nothing usable on these earbuds, hence the sweep. The
// connection is deliberately kept rather than closed and reopened: the earbuds
// refuse an immediate reconnection on the same channel until the previous
// RFCOMM session is torn down.
func dialAny(addr [6]byte) (rfcommConn, uint8, error) {
	if dialRFCOMM == nil {
		return nil, 0, ErrUnsupported
	}
	sawBusy := false
	timeouts := 0
	for ch := uint8(1); ch <= maxChannel; ch++ {
		c, err := dialRFCOMM(addr, ch, 3*time.Second)
		if err == nil {
			return c, ch, nil
		}
		// The device itself is absent: the 29 remaining channels will fail
		// the same way. Report the real cause instead of diluting it into
		// "no channel available".
		if errors.Is(err, ErrUnreachable) {
			return nil, 0, err
		}
		if errors.Is(err, ErrTimeout) {
			if timeouts++; timeouts == maxConsecutiveTimeouts {
				return nil, 0, ErrUnreachable
			}
			continue
		}
		timeouts = 0
		if errors.Is(err, ErrBusy) {
			sawBusy = true
		}
	}
	if sawBusy {
		return nil, 0, ErrBusy
	}
	return nil, 0, ErrNoChannel
}
