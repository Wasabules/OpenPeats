//go:build windows

package peats

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// RFCOMM transport on Windows: Winsock AF_BTH sockets.
//
// Validated on hardware (Windows 11, SOUNDPEATS H3): connection, reads and
// writes all confirmed. Getting there took two bugs that came straight from
// mirroring the Linux transport — a naturally aligned struct instead of a
// packed one, and a select() without exceptfds. Both are documented below,
// because both fail silently and look like an absent device.

const (
	afBTH          = 32
	bthprotoRFCOMM = 3

	fionbio = 0x8004667E

	// x/sys/windows does not export SO_ERROR; value taken from <winsock2.h>.
	soError = 0x1007

	wsaEWOULDBLOCK  = windows.Errno(10035)
	wsaETIMEDOUT    = windows.Errno(10060)
	wsaECONNREFUSED = windows.Errno(10061)

	// WSAEADDRNOTAVAIL: the device is paired but not connected — in its case,
	// powered off, out of range. Raised synchronously, on every channel.
	wsaEADDRNOTAVAIL = windows.Errno(10049)
	wsaEHOSTUNREACH  = windows.Errno(10065)

	// WSAEHOSTDOWN means a closed channel here, not an absent device:
	// measured on a reachable H3, where channels 1 and 12 return 10064 while
	// channel 11 accepts the connection. Treating it as "unreachable" would
	// abort the sweep at the very first channel.
	wsaEHOSTDOWN = windows.Errno(10064)
)

var (
	ws2         = windows.NewLazySystemDLL("ws2_32.dll")
	procConnect = ws2.NewProc("connect")
	procSelect  = ws2.NewProc("select")
	procIoctl   = ws2.NewProc("ioctlsocket")

	wsaOnce sync.Once
)

func init() { dialRFCOMM = dialWindows }

// sockaddrBTHLen is the size of SOCKADDR_BTH: 30 bytes, not 40.
//
// ws2bth.h includes <pshpack1.h> — the struct is packed to 1 byte, with no
// padding at all. An equivalent Go struct would be naturally aligned and put
// btAddr at offset 8 instead of 2: Windows would then read the address from
// the wrong place and reject every connect with WSAEADDRNOTAVAIL (10049), on
// every channel. Diagnosed on an H3, where the packed version connects first
// try.
const sockaddrBTHLen = 30

// encodeSockaddrBTH serialises SOCKADDR_BTH by hand, so nothing depends on the
// Go compiler's alignment rules.
//
//	offset 0  addressFamily  uint16
//	offset 2  btAddr         uint64
//	offset 10 serviceClassId GUID (16 bytes)
//	offset 26 port           uint32
func encodeSockaddrBTH(bt uint64, port uint32) [sockaddrBTHLen]byte {
	var sa [sockaddrBTHLen]byte
	binary.LittleEndian.PutUint16(sa[0:2], afBTH)
	binary.LittleEndian.PutUint64(sa[2:10], bt)
	// serviceClassId stays zero: the channel is addressed by number.
	binary.LittleEndian.PutUint32(sa[26:30], port)
	return sa
}

// Winsock's fd_set: a counter followed by an array of handles, not a bit mask
// like on POSIX.
type wsaFdSet struct {
	Count uint32
	Array [64]windows.Handle
}

type wsaTimeval struct {
	Sec  int32
	Usec int32
}

type windowsConn struct {
	mu     sync.Mutex
	sock   windows.Handle
	closed bool
}

func dialWindows(addr [6]byte, channel uint8, timeout time.Duration) (rfcommConn, error) {
	var startErr error
	wsaOnce.Do(func() {
		var d windows.WSAData
		startErr = windows.WSAStartup(uint32(0x202), &d)
	})
	if startErr != nil {
		return nil, fmt.Errorf("WSAStartup: %w", startErr)
	}

	sock, err := windows.Socket(afBTH, windows.SOCK_STREAM, bthprotoRFCOMM)
	if err != nil {
		return nil, fmt.Errorf("socket AF_BTH: %w", err)
	}

	// BTH_ADDR carries the 48-bit address in its low bytes, read in the
	// natural MAC order.
	var bt uint64
	for _, b := range addr {
		bt = bt<<8 | uint64(b)
	}

	sa := encodeSockaddrBTH(bt, uint32(channel))

	// Switch to non-blocking so the connect attempt can be bounded.
	nonblocking := uint32(1)
	if _, _, e := procIoctl.Call(uintptr(sock), uintptr(fionbio),
		uintptr(unsafe.Pointer(&nonblocking))); e != nil {
		if en, ok := e.(windows.Errno); ok && en != 0 {
			windows.Closesocket(sock)
			return nil, fmt.Errorf("ioctlsocket: %w", en)
		}
	}

	// LazyProc.Call already returns the GetLastError value, which matches
	// WSAGetLastError on modern Windows.
	r, _, callErr := procConnect.Call(uintptr(sock),
		uintptr(unsafe.Pointer(&sa[0])), uintptr(len(sa)))
	if r != 0 {
		lastErr, _ := callErr.(windows.Errno)
		if lastErr != wsaEWOULDBLOCK {
			windows.Closesocket(sock)
			return nil, mapWindowsErr(lastErr)
		}

		// Connection in progress: wait for the stack's verdict.
		//
		// ⚠ A real divergence from POSIX, not a detail: Winsock reports the
		// FAILURE of a non-blocking connect in exceptfds, never in writefds.
		// The Linux transport only watches writefds because the kernel there
		// makes the socket writable either way; copying that choice here would
		// wait out the full timeout on every closed channel — 90 s for a full
		// sweep — before wrongly concluding ErrTimeout.
		var w, ex wsaFdSet
		w.Count = 1
		w.Array[0] = sock
		ex.Count = 1
		ex.Array[0] = sock
		tv := wsaTimeval{
			Sec:  int32(timeout / time.Second),
			Usec: int32((timeout % time.Second) / time.Microsecond),
		}
		n, _, _ := procSelect.Call(0, 0,
			uintptr(unsafe.Pointer(&w)), uintptr(unsafe.Pointer(&ex)),
			uintptr(unsafe.Pointer(&tv)))
		if int32(n) <= 0 {
			windows.Closesocket(sock)
			return nil, ErrTimeout
		}

		soErr, gerr := windows.GetsockoptInt(sock, windows.SOL_SOCKET, soError)
		if gerr != nil {
			windows.Closesocket(sock)
			return nil, gerr
		}
		if soErr != 0 {
			windows.Closesocket(sock)
			return nil, mapWindowsErr(windows.Errno(soErr))
		}
	}

	// Back to blocking: reads are bounded by SO_RCVTIMEO.
	nonblocking = 0
	procIoctl.Call(uintptr(sock), uintptr(fionbio), uintptr(unsafe.Pointer(&nonblocking)))

	return &windowsConn{sock: sock}, nil
}

func mapWindowsErr(e windows.Errno) error {
	switch e {
	case wsaECONNREFUSED, wsaEHOSTDOWN:
		return ErrRefused
	case wsaETIMEDOUT:
		return ErrTimeout
	case windows.WSAEADDRINUSE, windows.WSAEISCONN:
		return ErrBusy
	case wsaEADDRNOTAVAIL, wsaEHOSTUNREACH:
		return ErrUnreachable
	default:
		return e
	}
}

func (c *windowsConn) handle() (windows.Handle, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return 0, ErrNotConnected
	}
	return c.sock, nil
}

func (c *windowsConn) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil // &p[0] would panic
	}
	s, err := c.handle()
	if err != nil {
		return 0, err
	}
	var n uint32
	var flags uint32
	buf := windows.WSABuf{Len: uint32(len(p)), Buf: &p[0]}
	if err := windows.WSARecv(s, &buf, 1, &n, &flags, nil, nil); err != nil {
		return 0, err
	}
	return int(n), nil
}

func (c *windowsConn) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	s, err := c.handle()
	if err != nil {
		return 0, err
	}
	var n uint32
	buf := windows.WSABuf{Len: uint32(len(p)), Buf: &p[0]}
	if err := windows.WSASend(s, &buf, 1, &n, 0, nil, nil); err != nil {
		return 0, err
	}
	return int(n), nil
}

// SetReadTimeout: on Windows, SO_RCVTIMEO takes a delay in milliseconds, not
// a timeval struct like on POSIX.
func (c *windowsConn) SetReadTimeout(d time.Duration) error {
	s, err := c.handle()
	if err != nil {
		return err
	}
	ms := int(d / time.Millisecond)
	return windows.SetsockoptInt(s, windows.SOL_SOCKET, windows.SO_RCVTIMEO, ms)
}

func (c *windowsConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	s := c.sock
	c.sock = 0
	return windows.Closesocket(s)
}

func isTimeout(err error) bool {
	return errors.Is(err, wsaETIMEDOUT) || errors.Is(err, wsaEWOULDBLOCK)
}
