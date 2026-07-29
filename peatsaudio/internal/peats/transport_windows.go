//go:build windows

package peats

import (
	"errors"
	"fmt"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Transport RFCOMM sous Windows : sockets Winsock AF_BTH.
//
// ⚠ NON TESTÉ. Écrit d'après la documentation Winsock Bluetooth de Microsoft,
// en miroir du transport Linux qui, lui, est validé sur matériel réel.
// Les écarts probables portent sur la gestion des délais et sur les codes
// d'erreur renvoyés par la pile Bluetooth de Windows.

const (
	afBTH          = 32
	bthprotoRFCOMM = 3

	fionbio = 0x8004667E

	// x/sys/windows n'expose pas SO_ERROR ; valeur de <winsock2.h>.
	soError = 0x1007

	wsaEWOULDBLOCK  = windows.Errno(10035)
	wsaETIMEDOUT    = windows.Errno(10060)
	wsaECONNREFUSED = windows.Errno(10061)
)

var (
	ws2         = windows.NewLazySystemDLL("ws2_32.dll")
	procConnect = ws2.NewProc("connect")
	procSelect  = ws2.NewProc("select")
	procIoctl   = ws2.NewProc("ioctlsocket")

	wsaOnce sync.Once
)

func init() { dialRFCOMM = dialWindows }

// SOCKADDR_BTH. Le champ btAddr est aligné sur 8 octets, d'où 6 octets de
// remplissage insérés par le compilateur après addressFamily.
type sockaddrBTH struct {
	AddressFamily  uint16
	BtAddr         uint64
	ServiceClassID [16]byte
	Port           uint32
}

// fd_set de Winsock : un compteur suivi d'un tableau de handles, et non un
// masque de bits comme sous POSIX.
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

	// BTH_ADDR porte l'adresse 48 bits dans les octets de poids faible, lue
	// dans l'ordre naturel de la MAC.
	var bt uint64
	for _, b := range addr {
		bt = bt<<8 | uint64(b)
	}

	sa := sockaddrBTH{
		AddressFamily: afBTH,
		BtAddr:        bt,
		Port:          uint32(channel),
	}

	// Passage en non bloquant pour borner l'attente de connexion.
	nonblocking := uint32(1)
	if _, _, e := procIoctl.Call(uintptr(sock), uintptr(fionbio),
		uintptr(unsafe.Pointer(&nonblocking))); e != nil {
		if en, ok := e.(windows.Errno); ok && en != 0 {
			windows.Closesocket(sock)
			return nil, fmt.Errorf("ioctlsocket: %w", en)
		}
	}

	// LazyProc.Call renvoie déjà l'erreur de GetLastError, qui coïncide avec
	// WSAGetLastError sur les Windows modernes.
	r, _, callErr := procConnect.Call(uintptr(sock),
		uintptr(unsafe.Pointer(&sa)), unsafe.Sizeof(sa))
	if r != 0 {
		lastErr, _ := callErr.(windows.Errno)
		if lastErr != wsaEWOULDBLOCK {
			windows.Closesocket(sock)
			return nil, mapWindowsErr(lastErr)
		}

		// Connexion en cours : on attend que le socket devienne inscriptible.
		var w wsaFdSet
		w.Count = 1
		w.Array[0] = sock
		tv := wsaTimeval{
			Sec:  int32(timeout / time.Second),
			Usec: int32((timeout % time.Second) / time.Microsecond),
		}
		n, _, _ := procSelect.Call(0, 0,
			uintptr(unsafe.Pointer(&w)), 0, uintptr(unsafe.Pointer(&tv)))
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

	// Retour en bloquant : les lectures sont bornées par SO_RCVTIMEO.
	nonblocking = 0
	procIoctl.Call(uintptr(sock), uintptr(fionbio), uintptr(unsafe.Pointer(&nonblocking)))

	return &windowsConn{sock: sock}, nil
}

func mapWindowsErr(e windows.Errno) error {
	switch e {
	case wsaECONNREFUSED:
		return ErrRefused
	case wsaETIMEDOUT:
		return ErrTimeout
	case windows.WSAEADDRINUSE, windows.WSAEISCONN:
		return ErrBusy
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

// SetReadTimeout : sous Windows, SO_RCVTIMEO attend un délai en millisecondes,
// et non une struct timeval comme sous POSIX.
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
