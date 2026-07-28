package peats

import (
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const btprotoRFCOMM = 3

// Réglages issus des mesures sur le H3.
const (
	// Une commande émise trop tôt après l'établissement du lien RFCOMM reste
	// sans réponse. ~600 ms suffisent.
	settleDelay = 700 * time.Millisecond

	defaultTimeout = 1500 * time.Millisecond
	readRetries    = 3
	maxChannel     = 30
)

var (
	// ErrBusy signale qu'un autre appareil monopolise le canal de contrôle.
	// Typiquement : multipoint actif et téléphone connecté avec l'app ouverte.
	ErrBusy = errors.New("canal de contrôle occupé par un autre appareil")

	// ErrNoChannel : aucun canal RFCOMM ouvert (écouteurs éteints ou en étui).
	ErrNoChannel = errors.New("aucun canal RFCOMM disponible")

	ErrTimeout      = errors.New("pas de réponse des écouteurs")
	ErrNotConnected = errors.New("non connecté")
)

type sockaddrRC struct {
	Family  uint16
	Bdaddr  [6]byte
	Channel uint8
	_       [1]byte
}

// ParseMAC convertit "98:80:BB:40:D4:ED" en bdaddr (ordre inverse).
func ParseMAC(s string) ([6]byte, error) {
	var out [6]byte
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) != 6 {
		return out, fmt.Errorf("adresse MAC invalide: %q", s)
	}
	for i, p := range parts {
		v, err := strconv.ParseUint(p, 16, 8)
		if err != nil {
			return out, fmt.Errorf("octet MAC invalide %q: %w", p, err)
		}
		out[5-i] = byte(v)
	}
	return out, nil
}

func fdSet(s *syscall.FdSet, fd int) {
	const bits = 64
	s.Bits[fd/bits] |= 1 << (uint(fd) % uint(bits))
}

func dial(addr [6]byte, channel uint8, timeout time.Duration) (int, error) {
	fd, err := syscall.Socket(syscall.AF_BLUETOOTH, syscall.SOCK_STREAM, btprotoRFCOMM)
	if err != nil {
		return -1, fmt.Errorf("socket: %w", err)
	}
	if err := syscall.SetNonblock(fd, true); err != nil {
		syscall.Close(fd)
		return -1, err
	}

	sa := sockaddrRC{Family: syscall.AF_BLUETOOTH, Bdaddr: addr, Channel: channel}
	_, _, errno := syscall.Syscall(syscall.SYS_CONNECT, uintptr(fd),
		uintptr(unsafe.Pointer(&sa)), unsafe.Sizeof(sa))
	if errno != 0 && errno != syscall.EINPROGRESS {
		syscall.Close(fd)
		return -1, errno
	}

	if errno == syscall.EINPROGRESS {
		var w syscall.FdSet
		fdSet(&w, fd)
		tv := syscall.NsecToTimeval(int64(timeout))
		n, err := syscall.Select(fd+1, nil, &w, nil, &tv)
		if err != nil {
			syscall.Close(fd)
			return -1, err
		}
		if n == 0 {
			syscall.Close(fd)
			return -1, ErrTimeout
		}
		soErr, err := syscall.GetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_ERROR)
		if err != nil {
			syscall.Close(fd)
			return -1, err
		}
		if soErr != 0 {
			syscall.Close(fd)
			return -1, syscall.Errno(soErr)
		}
	}

	if err := syscall.SetNonblock(fd, false); err != nil {
		syscall.Close(fd)
		return -1, err
	}
	return fd, nil
}

// dialAny balaie les canaux RFCOMM et renvoie la PREMIÈRE connexion établie,
// socket ouvert. sdptool ne renvoie rien d'utilisable sur ces écouteurs, d'où
// le balayage.
//
// Le socket est délibérément conservé plutôt que refermé puis rouvert : les
// écouteurs refusent une reconnexion immédiate sur le même canal (EBUSY) tant
// que la session RFCOMM précédente n'est pas démontée.
func dialAny(addr [6]byte) (int, uint8, error) {
	sawBusy := false
	for ch := uint8(1); ch <= maxChannel; ch++ {
		fd, err := dial(addr, ch, 3*time.Second)
		if err == nil {
			return fd, ch, nil
		}
		if errors.Is(err, syscall.EBUSY) {
			sawBusy = true
		}
	}
	if sawBusy {
		return -1, 0, ErrBusy
	}
	return -1, 0, ErrNoChannel
}

// Client est une connexion de contrôle vers une paire d'écouteurs.
type Client struct {
	mu      sync.Mutex
	fd      int
	channel uint8
	addr    [6]byte

	// exchange sérialise les échanges : un seul en vol à la fois.
	exMu sync.Mutex

	frames  chan Frame
	stopped chan struct{}
	closed  bool

	// OnFrame reçoit toutes les trames, y compris les notifications
	// spontanées émises par les écouteurs.
	OnFrame func(Frame)
}

// Connect ouvre une connexion de contrôle. channel = 0 déclenche la découverte.
func Connect(mac string, channel uint8) (*Client, error) {
	addr, err := ParseMAC(mac)
	if err != nil {
		return nil, err
	}

	var fd int
	if channel == 0 {
		fd, channel, err = dialAny(addr)
	} else {
		fd, err = dial(addr, channel, 10*time.Second)
	}
	if err != nil {
		if errors.Is(err, syscall.EBUSY) {
			return nil, ErrBusy
		}
		return nil, err
	}

	// Timeout de lecture court : la boucle de lecture reboucle dessus.
	tv := syscall.NsecToTimeval(int64(300 * time.Millisecond))
	if err := syscall.SetsockoptTimeval(fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &tv); err != nil {
		log.Printf("peats: SO_RCVTIMEO non appliqué: %v", err)
	}

	c := &Client{
		fd:      fd,
		channel: channel,
		addr:    addr,
		frames:  make(chan Frame, 32),
		stopped: make(chan struct{}),
	}
	go c.readLoop()

	// Laisser le lien se stabiliser avant la première commande.
	time.Sleep(settleDelay)
	return c, nil
}

func (c *Client) Channel() uint8 { return c.channel }

func (c *Client) readLoop() {
	defer close(c.frames)
	buf := make([]byte, 1024)
	var acc []byte

	for {
		select {
		case <-c.stopped:
			return
		default:
		}

		c.mu.Lock()
		fd := c.fd
		closed := c.closed
		c.mu.Unlock()
		if closed || fd < 0 {
			return
		}

		n, err := syscall.Read(fd, buf)
		if err != nil {
			// EAGAIN : timeout de lecture, normal. EINTR : préemption du
			// runtime Go sur un syscall bloquant.
			if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) ||
				errors.Is(err, syscall.EINTR) {
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
					acc = acc[used:] // bruit avant le SOF, on jette
				}
				break
			}
			acc = acc[used:]
			if c.OnFrame != nil {
				c.OnFrame(f)
			}
			select {
			case c.frames <- f:
			default: // consommateur en retard : on ne bloque pas la lecture
			}
		}
	}
}

// drain vide les trames en attente avant un nouvel échange, pour ne pas
// confondre une notification ancienne avec la réponse attendue.
func (c *Client) drain() {
	for {
		select {
		case <-c.frames:
		default:
			return
		}
	}
}

func (c *Client) write(b []byte) error {
	c.mu.Lock()
	fd, closed := c.fd, c.closed
	c.mu.Unlock()
	if closed || fd < 0 {
		return ErrNotConnected
	}
	_, err := syscall.Write(fd, b)
	return err
}

// await attend une trame satisfaisant match, dans la limite du timeout.
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

// Get envoie une commande de lecture et attend sa réponse.
// Les lectures sont réessayées : elles sont sans effet de bord.
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

// Set envoie une commande d'écriture.
//
// ⚠ Aucune réémission, jamais. Plusieurs `set` prennent effet sans acquitter
// (mode ANC, multipoint) ; renvoyer la commande rejouerait l'effet de bord —
// observé sur le H3 avec trois invites vocales pour une seule écriture voulue.
//
// L'acquittement, quand il arrive, porte l'opcode de réponse du `get`
// correspondant, pas celui du `set`. On l'attend brièvement à titre indicatif,
// mais son absence n'est PAS une erreur : l'appelant doit relire l'état.
func (c *Client) Set(setCmd, getCmd Command, payload []byte) (Frame, bool) {
	c.exMu.Lock()
	defer c.exMu.Unlock()

	c.drain()
	if err := c.write(Encode(setCmd, payload, VendorDefault)); err != nil {
		log.Printf("peats: échec d'écriture %s: %v", setCmd, err)
		return Frame{}, false
	}
	want := getCmd.Response()
	f, err := c.await(func(f Frame) bool { return f.Command == want }, defaultTimeout)
	return f, err == nil
}

// SetBool est un raccourci pour les bascules.
func (c *Client) SetBool(setCmd, getCmd Command, on bool) {
	var v byte
	if on {
		v = 1
	}
	c.Set(setCmd, getCmd, []byte{v})
}

// ApplyEQ envoie une courbe d'égaliseur, une trame par bande.
//
// La famille EQ utilise VendorEQ et répond avec EQResponse (0F 82), pas avec
// la convention « bit 7 » du reste du protocole.
func (c *Client) ApplyEQ(bands []EQBand, masterGain float64, v1 bool) error {
	if len(bands) == 0 {
		return errors.New("aucune bande à appliquer")
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
			return fmt.Errorf("bande %d: %w", i+1, err)
		}
		// L'ack est bref ; on l'attend sans en faire une condition d'échec.
		if _, err := c.await(func(f Frame) bool { return f.Command == EQResponse },
			defaultTimeout); err != nil {
			log.Printf("peats: bande %d sans ack (poursuite)", i+1)
		}
		time.Sleep(80 * time.Millisecond)
	}
	return nil
}

// Close ferme la connexion de contrôle.
func (c *Client) Close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	fd := c.fd
	c.fd = -1
	c.mu.Unlock()

	close(c.stopped)
	if fd >= 0 {
		syscall.Close(fd)
	}
}
