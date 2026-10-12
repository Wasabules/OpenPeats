package peats

import (
	"errors"
	"testing"
	"time"
)

// The sweep gives up on a device that lets every channel time out, instead of
// spending 30 × 3 s before saying it is not there.
func TestDialAnyStopsOnRepeatedTimeouts(t *testing.T) {
	attempts := 0
	prev := dialRFCOMM
	dialRFCOMM = func([6]byte, uint8, time.Duration) (rfcommConn, error) {
		attempts++
		return nil, ErrTimeout
	}
	defer func() { dialRFCOMM = prev }()

	if _, _, err := dialAny([6]byte{}); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("dialAny = %v, want ErrUnreachable", err)
	}
	if attempts != maxConsecutiveTimeouts {
		t.Errorf("%d channels tried, want %d", attempts, maxConsecutiveTimeouts)
	}
}

// A refusal in between resets the count: the device is there.
func TestDialAnyKeepsSweepingPastRefusals(t *testing.T) {
	fake := newFakeConn(nil)
	prev := dialRFCOMM
	dialRFCOMM = func(_ [6]byte, ch uint8, _ time.Duration) (rfcommConn, error) {
		switch {
		case ch == 11:
			return fake, nil
		case ch%3 == 0:
			return nil, ErrRefused
		default:
			return nil, ErrTimeout
		}
	}
	defer func() { dialRFCOMM = prev }()

	conn, ch, err := dialAny([6]byte{})
	if err != nil {
		t.Fatalf("dialAny: %v", err)
	}
	defer conn.Close()
	if ch != 11 {
		t.Errorf("connected on channel %d, want 11", ch)
	}
}
