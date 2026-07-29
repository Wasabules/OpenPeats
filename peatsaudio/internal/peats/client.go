package peats

import (
	"errors"
	"fmt"
	"log"
	"sync"
	"time"
)

// Réglages issus des mesures sur un SOUNDPEATS H3.
const (
	// Une commande émise trop tôt après l'établissement du lien RFCOMM reste
	// sans réponse. ~600 ms suffisent.
	settleDelay = 700 * time.Millisecond

	defaultTimeout = 1500 * time.Millisecond
	readRetries    = 3
	readTimeout    = 300 * time.Millisecond
)

// Client est une connexion de contrôle vers une paire d'écouteurs.
//
// Le transport est fourni par la couche plateforme (transport_*.go) ; tout ce
// qui suit est indépendant du système.
type Client struct {
	mu      sync.Mutex
	conn    rfcommConn
	channel uint8
	closed  bool

	// exMu sérialise les échanges : un seul en vol à la fois.
	exMu sync.Mutex

	frames  chan Frame
	stopped chan struct{}

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
		log.Printf("peats: délai de lecture non appliqué: %v", err)
	}

	c := &Client{
		conn:    conn,
		channel: channel,
		frames:  make(chan Frame, 32),
		stopped: make(chan struct{}),
	}
	go c.readLoop()

	// Laisser le lien se stabiliser avant la première commande.
	time.Sleep(settleDelay)
	return c, nil
}

func (c *Client) Channel() uint8 { return c.channel }

func (c *Client) transport() (rfcommConn, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.conn == nil {
		return nil, ErrNotConnected
	}
	return c.conn, nil
}

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
			default: // consommateur en retard : ne pas bloquer la lecture
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
	conn, err := c.transport()
	if err != nil {
		return err
	}
	_, err = conn.Write(b)
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
// L'acquittement, quand il arrive, porte généralement l'opcode de réponse du
// `get` correspondant plutôt que celui du `set`. Son absence n'est PAS une
// erreur : l'appelant doit relire l'état pour vérifier.
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
	conn := c.conn
	c.conn = nil
	c.mu.Unlock()

	close(c.stopped)
	if conn != nil {
		conn.Close()
	}
}
