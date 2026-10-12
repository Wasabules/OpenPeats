//go:build linux

// Probe validating the PeatsAudio protocol over RFCOMM/SPP.
//
// Purpose: confirm on the wire what PROTOCOL.md describes from static
// analysis. Sends one or more commands over a single connection, prints the
// frames it emits and decodes the replies.
//
// Read-only by default: any command outside the allow-list requires
// -allow-write.
//
// No external dependency - AF_BLUETOOTH/BTPROTO_RFCOMM sockets via syscall.
//
// This is an exploration tool, not a driver, and its output is meant for
// whoever is reverse-engineering the protocol: English only, no i18n. The
// applications are where translated strings belong.
package main

import (
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const (
	btprotoRFCOMM = 3

	sof   = 0xFF
	flags = 0x00

	// The app sends its requests as version 0x04; the earbuds answer with
	// 0x03. Confirmed on the wire (H3).
	versionRequest  = 0x04
	versionResponse = 0x03
)

var (
	vendorDefault = [2]byte{0x00, 0x0A}
	vendorEQ      = [2]byte{0x00, 0x1D}
)

// Known read commands - safe, they write nothing to the earbuds.
var readOnlyCmds = map[[2]byte]string{
	{0x03, 0x06}: "left battery",
	{0x03, 0x07}: "right battery",
	{0x03, 0x23}: "case battery",
	{0x03, 0x09}: "firmware version",
	{0x03, 0x10}: "ANC mode",
	{0x03, 0x24}: "ANC level",
	{0x03, 0x4C}: "transparency level",
	{0x03, 0x0C}: "in-ear detection",
	{0x03, 0x0E}: "game mode",
	{0x03, 0x14}: "multipoint",
	{0x03, 0x38}: "LDAC",
	{0x03, 0x35}: "LE Audio",
	{0x03, 0xAA}: "touch mapping",
	{0x03, 0x12}: "touch disabled",
	{0x03, 0x0A}: "light effect",
	{0x03, 0x27}: "movie mode",
	{0x03, 0x29}: "panoramic sound",
	{0x03, 0x31}: "dynamic EQ",
	{0x03, 0x3C}: "dynamic bass",
	{0x03, 0x48}: "Dolby",
	{0x03, 0x57}: "spatial effect",
	{0x03, 0x19}: "prompt volume",
	{0x03, 0x21}: "voice pack",
	{0x03, 0x42}: "left ear volume",
	{0x03, 0x43}: "right ear volume",
	{0x03, 0x40}: "personalised volume",
	{0x03, 0x46}: "adaptive volume",
	{0x03, 0x50}: "water drain",
	{0x03, 0x55}: "fall detection",
	{0x03, 0x6B}: "privacy mode",
	{0x03, 0x17}: "classic BT connection",
}

type sockaddrRC struct {
	Family  uint16
	Bdaddr  [6]byte
	Channel uint8
	_       [1]byte
}

func parseMAC(s string) ([6]byte, error) {
	var out [6]byte
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) != 6 {
		return out, fmt.Errorf("invalid MAC address: %q", s)
	}
	// bdaddr is stored in reverse order (little-endian).
	for i, p := range parts {
		v, err := strconv.ParseUint(p, 16, 8)
		if err != nil {
			return out, fmt.Errorf("invalid MAC byte %q: %w", p, err)
		}
		out[5-i] = byte(v)
	}
	return out, nil
}

// fdSet sets the bit matching the descriptor in an FdSet.
func fdSet(s *syscall.FdSet, fd int) {
	bits := 64 // size of one FdSet.Bits element on linux/amd64
	s.Bits[fd/bits] |= 1 << (uint(fd) % uint(bits))
}

// connectRFCOMM opens an RFCOMM socket with a real connection timeout
// (non-blocking connect + select).
func connectRFCOMM(addr [6]byte, channel uint8, timeout time.Duration) (int, error) {
	fd, err := syscall.Socket(syscall.AF_BLUETOOTH, syscall.SOCK_STREAM, btprotoRFCOMM)
	if err != nil {
		return -1, fmt.Errorf("socket: %w", err)
	}

	if err := syscall.SetNonblock(fd, true); err != nil {
		syscall.Close(fd)
		return -1, fmt.Errorf("setnonblock: %w", err)
	}

	sa := sockaddrRC{Family: syscall.AF_BLUETOOTH, Bdaddr: addr, Channel: channel}
	_, _, errno := syscall.Syscall(
		syscall.SYS_CONNECT, uintptr(fd),
		uintptr(unsafe.Pointer(&sa)), unsafe.Sizeof(sa),
	)
	if errno != 0 && errno != syscall.EINPROGRESS {
		syscall.Close(fd)
		return -1, fmt.Errorf("connect: %w", errno)
	}

	if errno == syscall.EINPROGRESS {
		var w syscall.FdSet
		fdSet(&w, fd)
		tv := syscall.NsecToTimeval(int64(timeout))
		n, err := syscall.Select(fd+1, nil, &w, nil, &tv)
		if err != nil {
			syscall.Close(fd)
			return -1, fmt.Errorf("select: %w", err)
		}
		if n == 0 {
			syscall.Close(fd)
			return -1, errors.New("connection timed out")
		}
		soErr, err := syscall.GetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_ERROR)
		if err != nil {
			syscall.Close(fd)
			return -1, fmt.Errorf("getsockopt: %w", err)
		}
		if soErr != 0 {
			syscall.Close(fd)
			return -1, fmt.Errorf("connect: %w", syscall.Errno(soErr))
		}
	}

	if err := syscall.SetNonblock(fd, false); err != nil {
		syscall.Close(fd)
		return -1, fmt.Errorf("setnonblock(false): %w", err)
	}
	return fd, nil
}

// buildFrame assembles a PeatsAudio frame (see PROTOCOL.md section 2).
//
//	FF | 04 | 00 | len(payload) | vendorId(2) | command(2) | payload
func buildFrame(cmd [2]byte, payload []byte, vendor [2]byte) []byte {
	f := make([]byte, 0, 8+len(payload))
	f = append(f, sof, versionRequest, flags, byte(len(payload)))
	f = append(f, vendor[0], vendor[1])
	f = append(f, cmd[0], cmd[1])
	return append(f, payload...)
}

type frame struct {
	SOF     byte
	Version byte
	Flags   byte
	Length  byte
	Vendor  [2]byte
	Command [2]byte
	Payload []byte
}

func (f frame) String() string {
	return fmt.Sprintf(
		"v=%02X len=%d vendor=%02X%02X cmd=%02X%02X payload=%s",
		f.Version, f.Length, f.Vendor[0], f.Vendor[1],
		f.Command[0], f.Command[1],
		strings.ToUpper(hex.EncodeToString(f.Payload)),
	)
}

// parseFrames splits an RFCOMM stream into frames. Returns the complete
// frames and the unconsumed remainder.
func parseFrames(buf []byte) ([]frame, []byte) {
	var out []frame
	for {
		// Resynchronise on the SOF.
		i := 0
		for i < len(buf) && buf[i] != sof {
			i++
		}
		buf = buf[i:]

		if len(buf) < 8 {
			return out, buf
		}
		total := 8 + int(buf[3])
		if len(buf) < total {
			return out, buf
		}

		out = append(out, frame{
			SOF: buf[0], Version: buf[1], Flags: buf[2], Length: buf[3],
			Vendor:  [2]byte{buf[4], buf[5]},
			Command: [2]byte{buf[6], buf[7]},
			Payload: append([]byte(nil), buf[8:total]...),
		})
		buf = buf[total:]
	}
}

var ancModes = map[byte]string{
	0x00: "normal (ANC off)", 0x01: "ANC on", 0x02: "transparency",
	0x03: "adaptive ANC", 0x04: "weak ANC", 0xFF: "undefined",
}

var ancLevels = map[byte]string{
	0x01: "level 1", 0x02: "level 2", 0x03: "level 3",
	0x04: "level 4", 0x05: "level 5",
	0x11: "adaptive", 0x12: "indoor", 0x13: "outdoor",
	0x14: "traffic", 0x15: "adaptive reduction", 0x16: "anti-wind",
}

var transparentLevels = map[byte]string{
	0x01: "voice boost", 0x02: "standard transparency",
}

// describePairs decodes list replies: one header byte, then (code, active)
// pairs. Confirmed on the wire by writing 03 25 = 14 and watching the flag
// move onto the matching pair.
func describePairs(p []byte, names map[byte]string) string {
	if len(p) < 3 {
		return ""
	}
	var avail []string
	var active string
	for i := 1; i+1 < len(p); i += 2 {
		name := names[p[i]]
		if name == "" {
			name = fmt.Sprintf("0x%02X", p[i])
		}
		avail = append(avail, name)
		if p[i+1] == 0x01 {
			active = name
		}
	}
	if active == "" {
		active = "none"
	}
	return fmt.Sprintf("active=%s | available: %s", active, strings.Join(avail, ", "))
}

var keyGestures = map[byte]string{
	0x01: "tap L", 0x02: "tap R", 0x03: "double tap L", 0x04: "double tap R",
	0x05: "triple tap L", 0x06: "triple tap R", 0x07: "long press L", 0x08: "long press R",
}

var keyActions = map[byte]string{
	0x01: "volume +", 0x02: "volume -", 0x03: "play/pause", 0x04: "game mode",
	0x05: "prev track", 0x06: "next track", 0x07: "ANC mode", 0x08: "voice assistant",
}

// describe gives a human reading of the payload when we know how to.
func describe(f frame) string {
	if f.Command[0]&0x80 == 0 {
		return ""
	}
	base := [2]byte{f.Command[0] &^ 0x80, f.Command[1]}
	p := f.Payload
	if len(p) == 0 {
		return ""
	}
	// Confirmed on the wire: scalar payloads are two big-endian bytes. The
	// useful value sits in the last byte.
	v := p[len(p)-1]

	switch base {
	case [2]byte{0x03, 0x06}, [2]byte{0x03, 0x07}, [2]byte{0x03, 0x23}:
		if v <= 100 {
			return fmt.Sprintf("%d %%", v)
		}
		return fmt.Sprintf("out of battery range (%d)", v)

	case [2]byte{0x03, 0x09}:
		// One status byte, then the version in ASCII.
		return "firmware: " + strings.TrimRight(string(p[1:]), "\x00")

	case [2]byte{0x03, 0x10}:
		if s, ok := ancModes[v]; ok {
			return s
		}

	case [2]byte{0x03, 0x24}:
		return describePairs(p, ancLevels)

	case [2]byte{0x03, 0x4C}:
		return describePairs(p, transparentLevels)

	case [2]byte{0x03, 0xAA}:
		// Header byte then [gesture, action] pairs.
		var out []string
		for i := 1; i+1 < len(p); i += 2 {
			g, a := keyGestures[p[i]], keyActions[p[i+1]]
			if g == "" {
				g = fmt.Sprintf("0x%02X", p[i])
			}
			if a == "" {
				a = fmt.Sprintf("0x%02X", p[i+1])
			}
			out = append(out, g+"→"+a)
		}
		return strings.Join(out, ", ")

	case [2]byte{0x03, 0x38}, [2]byte{0x03, 0x35}, [2]byte{0x03, 0x0C},
		[2]byte{0x03, 0x0E}, [2]byte{0x03, 0x14}, [2]byte{0x03, 0x12},
		[2]byte{0x03, 0x0A}, [2]byte{0x03, 0x27}, [2]byte{0x03, 0x31},
		[2]byte{0x03, 0x3C}, [2]byte{0x03, 0x48}, [2]byte{0x03, 0x57},
		[2]byte{0x03, 0x40}, [2]byte{0x03, 0x46}, [2]byte{0x03, 0x50},
		[2]byte{0x03, 0x55}, [2]byte{0x03, 0x6B}:
		switch v {
		case 0x00:
			return "off"
		case 0x01:
			return "on"
		}
	}
	return ""
}

func parseCmd(s string) ([2]byte, error) {
	var c [2]byte
	clean := strings.ReplaceAll(strings.ReplaceAll(s, " ", ""), "0x", "")
	b, err := hex.DecodeString(clean)
	if err != nil {
		return c, fmt.Errorf("invalid hex command %q: %w", s, err)
	}
	if len(b) != 2 {
		return c, fmt.Errorf("the command must be 2 bytes, got %d", len(b))
	}
	return [2]byte{b[0], b[1]}, nil
}

// readAvailable drains the socket for the given window.
func readAvailable(fd int, window time.Duration, acc []byte) ([]frame, []byte, []byte) {
	deadline := time.Now().Add(window)
	buf := make([]byte, 1024)
	var raw []byte
	var all []frame

	for time.Now().Before(deadline) {
		n, err := syscall.Read(fd, buf)
		if err != nil {
			// EINTR: the Go runtime interrupts blocking syscalls for
			// preemption - just retry.
			if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) ||
				errors.Is(err, syscall.EINTR) {
				continue
			}
			break
		}
		if n == 0 {
			break
		}
		raw = append(raw, buf[:n]...)
		acc = append(acc, buf[:n]...)
		var fs []frame
		fs, acc = parseFrames(acc)
		all = append(all, fs...)
		if len(all) > 0 {
			break // got a reply, no point waiting out the whole window
		}
	}
	return all, acc, raw
}

// exchange sends a command and waits for a reply, with retries.
func exchange(fd int, cmd [2]byte, payload []byte, vendor [2]byte,
	window time.Duration, retries int, verbose bool) ([]frame, []byte) {

	var acc []byte
	var lastRaw []byte
	out := buildFrame(cmd, payload, vendor)

	for try := 1; try <= retries; try++ {
		if verbose {
			fmt.Printf("  → %s\n", strings.ToUpper(hex.EncodeToString(out)))
		}
		if _, err := syscall.Write(fd, out); err != nil {
			fmt.Fprintln(os.Stderr, "  write failed:", err)
			return nil, nil
		}
		var fs []frame
		var raw []byte
		fs, acc, raw = readAvailable(fd, window, acc)
		if len(raw) > 0 {
			lastRaw = raw
		}
		if len(fs) > 0 {
			return fs, lastRaw
		}
		if verbose && try < retries {
			fmt.Printf("  (attempt %d got no reply, retrying)\n", try)
		}
	}
	return nil, lastRaw
}

// scanChannels looks for RFCOMM channels that accept a connection. Every
// connection is closed right away: this is the -scan listing, not a way to pick
// a channel to talk on (see firstOpenChannel).
func scanChannels(addr [6]byte, max int, timeout time.Duration) []uint8 {
	var found []uint8
	for ch := 1; ch <= max; ch++ {
		fd, err := connectRFCOMM(addr, uint8(ch), timeout)
		if err == nil {
			found = append(found, uint8(ch))
			syscall.Close(fd)
			fmt.Printf("  channel %2d: open\n", ch)
		} else if !errors.Is(err, syscall.ECONNREFUSED) {
			fmt.Printf("  channel %2d: %v\n", ch, err)
		}
	}
	return found
}

// firstOpenChannel sweeps the channels and keeps the first connection that
// succeeds. Closing it to reopen the same channel straight after would fail:
// the earbuds refuse an immediate reconnection until the previous RFCOMM
// session is torn down — the trap the client's dialAny avoids the same way.
func firstOpenChannel(addr [6]byte, max int, timeout time.Duration) (int, uint8, error) {
	for ch := 1; ch <= max; ch++ {
		fd, err := connectRFCOMM(addr, uint8(ch), timeout)
		if err == nil {
			return fd, uint8(ch), nil
		}
		if !errors.Is(err, syscall.ECONNREFUSED) {
			fmt.Printf("  channel %2d: %v\n", ch, err)
		}
	}
	return -1, 0, errors.New("no open RFCOMM channel")
}

func main() {
	var (
		addrStr    = flag.String("addr", "", "MAC address of the earbuds (required)")
		channel    = flag.Int("channel", 0, "RFCOMM channel (0 = auto-discovery)")
		cmdStr     = flag.String("cmd", "0306", "2-byte hex command(s), comma separated")
		payloadStr = flag.String("payload", "", "hex payload (applied to every command)")
		eqVendor   = flag.Bool("eq-vendor", false, "use the EQ vendorId 001D instead of 000A")
		allowWrite = flag.Bool("allow-write", false, "allow commands outside the allow-list")
		scan       = flag.Bool("scan", false, "only scan the RFCOMM channels")
		maxChan    = flag.Int("max-channel", 30, "highest channel explored")
		window     = flag.Duration("window", 1500*time.Millisecond, "wait window per attempt")
		retries    = flag.Int("retry", 3, "attempts per command")
		settle     = flag.Duration("settle", 600*time.Millisecond, "delay after connecting before sending")
		gap        = flag.Duration("gap", 250*time.Millisecond, "delay between two commands")
		connTO     = flag.Duration("connect-timeout", 10*time.Second, "connection timeout")
		quiet      = flag.Bool("quiet", false, "compact output (one line per command)")
		listen     = flag.Duration("listen", 0, "after the commands, listen for spontaneous frames for this long")
	)
	flag.Parse()

	// No default: one would point every run at a particular pair of earbuds.
	if strings.TrimSpace(*addrStr) == "" {
		fmt.Fprintln(os.Stderr, "error: -addr is required (e.g. -addr AA:BB:CC:DD:EE:FF)")
		os.Exit(2)
	}
	addr, err := parseMAC(*addrStr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(2)
	}

	// --- Channel listing only ---
	if *scan {
		fmt.Printf("Discovering RFCOMM channels on %s (1..%d)...\n", *addrStr, *maxChan)
		found := scanChannels(addr, *maxChan, *connTO)
		if len(found) == 0 {
			fmt.Fprintln(os.Stderr, "\nNo open RFCOMM channel.")
			fmt.Fprintln(os.Stderr, "Are the earbuds powered on and out of their case?")
			os.Exit(1)
		}
		fmt.Printf("Open channels: %v\n", found)
		return
	}

	// --- Commands + read-only guard ---
	var cmds [][2]byte
	var labels []string
	for _, s := range strings.Split(*cmdStr, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		c, err := parseCmd(s)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(2)
		}
		label, known := readOnlyCmds[c]
		if !known {
			if !*allowWrite {
				fmt.Fprintf(os.Stderr,
					"Refused: %02X%02X is not a known read command.\n"+
						"This probe is read-only. Pass -allow-write to force it.\n",
					c[0], c[1])
				os.Exit(2)
			}
			label = "outside the allow-list"
		}
		cmds = append(cmds, c)
		labels = append(labels, label)
	}
	if len(cmds) == 0 {
		fmt.Fprintln(os.Stderr, "no command to send")
		os.Exit(2)
	}

	var payload []byte
	if *payloadStr != "" {
		payload, err = hex.DecodeString(strings.ReplaceAll(*payloadStr, " ", ""))
		if err != nil {
			fmt.Fprintln(os.Stderr, "invalid hex payload:", err)
			os.Exit(2)
		}
	}

	vendor := vendorDefault
	if *eqVendor {
		vendor = vendorEQ
	}

	// --- Connection (a single one, reused) ---
	// Commands are validated first, so a refused command never costs a
	// connection.
	ch := uint8(*channel)
	var fd int
	if ch == 0 {
		fmt.Printf("Looking for an open RFCOMM channel on %s (1..%d)...\n", *addrStr, *maxChan)
		fd, ch, err = firstOpenChannel(addr, *maxChan, *connTO)
		if err != nil {
			fmt.Fprintln(os.Stderr, "\nNo open RFCOMM channel.")
			fmt.Fprintln(os.Stderr, "Are the earbuds powered on and out of their case?")
			os.Exit(1)
		}
	} else {
		fd, err = connectRFCOMM(addr, ch, *connTO)
		if err != nil {
			fmt.Fprintln(os.Stderr, "connection failed:", err)
			os.Exit(1)
		}
	}
	defer syscall.Close(fd)
	fmt.Printf("Connected on channel %d.\n", ch)

	tv := syscall.NsecToTimeval(int64(300 * time.Millisecond))
	if err := syscall.SetsockoptTimeval(fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &tv); err != nil {
		fmt.Fprintln(os.Stderr, "warning: SO_RCVTIMEO not applied:", err)
	}

	// Let the link settle: without this delay the first commands frequently
	// get no reply.
	time.Sleep(*settle)

	ok := 0
	for i, c := range cmds {
		if i > 0 {
			time.Sleep(*gap)
		}
		if !*quiet {
			fmt.Printf("\n=== %02X%02X — %s ===\n", c[0], c[1], labels[i])
		}

		// Writes are NEVER retried: several `set` commands take effect
		// without acknowledging (ANC mode, multipoint). A retry would replay
		// the side effect — observed on the H3 as three "passthrough" voice
		// prompts for a single intended write.
		// To check a `set` landed, read the state back, do not resend.
		tries := *retries
		if _, isRead := readOnlyCmds[c]; !isRead {
			tries = 1
		}
		frames, raw := exchange(fd, c, payload, vendor, *window, tries, !*quiet)

		if len(frames) == 0 {
			if *quiet {
				fmt.Printf("%02X%02X  %-28s  NO REPLY\n", c[0], c[1], labels[i])
			} else {
				fmt.Printf("  NO REPLY after %d attempts\n", *retries)
				if len(raw) > 0 {
					fmt.Printf("  undecoded bytes: %s\n",
						strings.ToUpper(hex.EncodeToString(raw)))
				}
			}
			continue
		}
		ok++
		for _, f := range frames {
			d := describe(f)
			if *quiet {
				fmt.Printf("%02X%02X  %-28s  %-22s %s\n", c[0], c[1], labels[i],
					strings.ToUpper(hex.EncodeToString(f.Payload)), d)
			} else {
				fmt.Printf("  ← %s\n", f)
				if d != "" {
					fmt.Printf("    → %s\n", d)
				}
				if f.SOF != sof ||
					(f.Version != versionRequest && f.Version != versionResponse) {
					fmt.Printf("    ! unexpected header (sof=%02X version=%02X)\n",
						f.SOF, f.Version)
				}
			}
		}
	}

	fmt.Printf("\n--- %d/%d command(s) answered ---\n", ok, len(cmds))

	if *listen > 0 {
		fmt.Printf("\n=== listening for spontaneous frames for %s ===\n", *listen)
		fmt.Println("(tap the earbuds now)")
		var acc []byte
		buf := make([]byte, 1024)
		deadline := time.Now().Add(*listen)
		n := 0

		for time.Now().Before(deadline) {
			r, err := syscall.Read(fd, buf)
			if err != nil {
				if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) ||
					errors.Is(err, syscall.EINTR) {
					continue
				}
				fmt.Fprintln(os.Stderr, "read:", err)
				break
			}
			if r == 0 {
				fmt.Fprintln(os.Stderr, "connection closed by the device")
				break
			}
			acc = append(acc, buf[:r]...)
			var fs []frame
			fs, acc = parseFrames(acc)
			for _, f := range fs {
				n++
				fmt.Printf("  [%s] %s\n", time.Now().Format("15:04:05.000"), f)
				if d := describe(f); d != "" {
					fmt.Printf("            → %s\n", d)
				}
			}
		}
		fmt.Printf("\n--- %d spontaneous frame(s) ---\n", n)
		if n == 0 {
			fmt.Println("No frames: the earbuds do not report taps on this channel.")
		}
		return
	}

	if ok == 0 {
		os.Exit(1)
	}
}
