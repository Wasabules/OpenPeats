//go:build linux

package peats

import (
	"errors"
	"fmt"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// Transport RFCOMM sous Linux : sockets AF_BLUETOOTH natifs du noyau.
// Aucune dépendance externe, aucun démon à interroger.
//
// Testé sur Ubuntu 24.04 contre un SOUNDPEATS H3.

const btprotoRFCOMM = 3

func init() { dialRFCOMM = dialLinux }

// sockaddr_rc, tel que défini par <bluetooth/rfcomm.h>.
type sockaddrRC struct {
	Family  uint16
	Bdaddr  [6]byte
	Channel uint8
	_       [1]byte
}

type linuxConn struct {
	mu     sync.Mutex
	fd     int
	closed bool
}

func fdSet(s *syscall.FdSet, fd int) {
	const bits = 64
	s.Bits[fd/bits] |= 1 << (uint(fd) % uint(bits))
}

func dialLinux(addr [6]byte, channel uint8, timeout time.Duration) (rfcommConn, error) {
	fd, err := syscall.Socket(syscall.AF_BLUETOOTH, syscall.SOCK_STREAM, btprotoRFCOMM)
	if err != nil {
		return nil, fmt.Errorf("socket: %w", err)
	}
	if err := syscall.SetNonblock(fd, true); err != nil {
		syscall.Close(fd)
		return nil, err
	}

	// bdaddr est stocké en ordre inverse.
	var bd [6]byte
	for i := 0; i < 6; i++ {
		bd[i] = addr[5-i]
	}

	sa := sockaddrRC{Family: syscall.AF_BLUETOOTH, Bdaddr: bd, Channel: channel}
	_, _, errno := syscall.Syscall(syscall.SYS_CONNECT, uintptr(fd),
		uintptr(unsafe.Pointer(&sa)), unsafe.Sizeof(sa))
	if errno != 0 && errno != syscall.EINPROGRESS {
		syscall.Close(fd)
		return nil, mapLinuxErr(errno)
	}

	if errno == syscall.EINPROGRESS {
		var w syscall.FdSet
		fdSet(&w, fd)
		tv := syscall.NsecToTimeval(int64(timeout))
		n, err := syscall.Select(fd+1, nil, &w, nil, &tv)
		if err != nil {
			syscall.Close(fd)
			return nil, err
		}
		if n == 0 {
			syscall.Close(fd)
			return nil, ErrTimeout
		}
		soErr, err := syscall.GetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_ERROR)
		if err != nil {
			syscall.Close(fd)
			return nil, err
		}
		if soErr != 0 {
			syscall.Close(fd)
			return nil, mapLinuxErr(syscall.Errno(soErr))
		}
	}

	if err := syscall.SetNonblock(fd, false); err != nil {
		syscall.Close(fd)
		return nil, err
	}
	return &linuxConn{fd: fd}, nil
}

func mapLinuxErr(errno syscall.Errno) error {
	switch errno {
	case syscall.EBUSY:
		return ErrBusy
	case syscall.ECONNREFUSED:
		return ErrRefused
	default:
		return errno
	}
}

func (c *linuxConn) Read(p []byte) (int, error) {
	c.mu.Lock()
	fd, closed := c.fd, c.closed
	c.mu.Unlock()
	if closed {
		return 0, ErrNotConnected
	}
	return syscall.Read(fd, p)
}

func (c *linuxConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	fd, closed := c.fd, c.closed
	c.mu.Unlock()
	if closed {
		return 0, ErrNotConnected
	}
	return syscall.Write(fd, p)
}

func (c *linuxConn) SetReadTimeout(d time.Duration) error {
	c.mu.Lock()
	fd, closed := c.fd, c.closed
	c.mu.Unlock()
	if closed {
		return ErrNotConnected
	}
	tv := syscall.NsecToTimeval(int64(d))
	return syscall.SetsockoptTimeval(fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &tv)
}

func (c *linuxConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	fd := c.fd
	c.fd = -1
	return syscall.Close(fd)
}

// isTimeout distingue un dépassement du délai de lecture d'une vraie erreur.
// EINTR apparaît quand le runtime Go préempte un syscall bloquant.
func isTimeout(err error) bool {
	return errors.Is(err, syscall.EAGAIN) ||
		errors.Is(err, syscall.EWOULDBLOCK) ||
		errors.Is(err, syscall.EINTR)
}
