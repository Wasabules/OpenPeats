// Sonde de validation du protocole PeatsAudio sur RFCOMM/SPP.
//
// Objectif : confirmer sur le fil ce que PROTOCOL.md décrit par analyse statique.
// Envoie une ou plusieurs commandes sur une même connexion, affiche les trames
// émises et décode les réponses.
//
// Lecture seule par défaut : toute commande hors de la liste blanche exige -allow-write.
//
// Aucune dépendance externe — sockets AF_BLUETOOTH/BTPROTO_RFCOMM via syscall.
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

	// L'app émet ses requêtes en version 0x04 ; les écouteurs répondent en 0x03.
	// Confirmé sur le fil (H3).
	versionRequest  = 0x04
	versionResponse = 0x03
)

var (
	vendorDefault = [2]byte{0x00, 0x0A}
	vendorEQ      = [2]byte{0x00, 0x1D}
)

// Commandes de lecture connues — sûres, elles n'écrivent rien dans les écouteurs.
var readOnlyCmds = map[[2]byte]string{
	{0x03, 0x06}: "batterie gauche",
	{0x03, 0x07}: "batterie droite",
	{0x03, 0x23}: "batterie étui",
	{0x03, 0x09}: "version firmware",
	{0x03, 0x10}: "mode ANC",
	{0x03, 0x24}: "niveau ANC",
	{0x03, 0x4C}: "niveau transparence",
	{0x03, 0x0C}: "détection intra-auriculaire",
	{0x03, 0x0E}: "mode jeu",
	{0x03, 0x14}: "multipoint",
	{0x03, 0x38}: "LDAC",
	{0x03, 0x35}: "LE Audio",
	{0x03, 0xAA}: "commandes tactiles",
	{0x03, 0x12}: "tactile désactivé",
	{0x03, 0x0A}: "effet lumineux",
	{0x03, 0x27}: "mode cinéma",
	{0x03, 0x29}: "son panoramique",
	{0x03, 0x31}: "EQ dynamique",
	{0x03, 0x3C}: "basses dynamiques",
	{0x03, 0x48}: "Dolby",
	{0x03, 0x57}: "effet spatial",
	{0x03, 0x19}: "volume des invites",
	{0x03, 0x21}: "pack vocal",
	{0x03, 0x42}: "volume oreille gauche",
	{0x03, 0x43}: "volume oreille droite",
	{0x03, 0x40}: "volume personnalisé",
	{0x03, 0x46}: "volume adaptatif",
	{0x03, 0x50}: "évacuation d'eau",
	{0x03, 0x55}: "détection de chute",
	{0x03, 0x6B}: "mode privé",
	{0x03, 0x17}: "connexion BT classique",
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
		return out, fmt.Errorf("adresse MAC invalide: %q", s)
	}
	// bdaddr est en ordre inverse (little-endian).
	for i, p := range parts {
		v, err := strconv.ParseUint(p, 16, 8)
		if err != nil {
			return out, fmt.Errorf("octet MAC invalide %q: %w", p, err)
		}
		out[5-i] = byte(v)
	}
	return out, nil
}

// fdSet arme le bit correspondant au descripteur dans un FdSet.
func fdSet(s *syscall.FdSet, fd int) {
	bits := 64 // taille d'un élément de FdSet.Bits sur linux/amd64
	s.Bits[fd/bits] |= 1 << (uint(fd) % uint(bits))
}

// connectRFCOMM ouvre un socket RFCOMM avec un vrai timeout de connexion
// (connect non bloquant + select).
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
			return -1, errors.New("timeout de connexion")
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

// buildFrame assemble une trame PeatsAudio (cf. PROTOCOL.md §2).
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

// parseFrames découpe un flux RFCOMM en trames. Retourne les trames complètes
// et le reliquat non consommé.
func parseFrames(buf []byte) ([]frame, []byte) {
	var out []frame
	for {
		// Resynchronisation sur le SOF.
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
	0x00: "normal (ANC arrêté)", 0x01: "ANC actif", 0x02: "transparence",
	0x03: "ANC adaptatif", 0x04: "ANC faible", 0xFF: "non défini",
}

var ancLevels = map[byte]string{
	0x01: "niveau 1", 0x02: "niveau 2", 0x03: "niveau 3",
	0x04: "niveau 4", 0x05: "niveau 5",
	0x11: "adaptatif", 0x12: "intérieur", 0x13: "extérieur",
	0x14: "trafic", 0x15: "réduction adaptative", 0x16: "anti-vent",
}

var transparentLevels = map[byte]string{
	0x01: "voix renforcée", 0x02: "transparence standard",
}

// describePairs décode les réponses en liste : un octet de tête, puis des paires
// (code, actif). Confirmé sur le fil en écrivant 03 25 = 14 et en observant le
// drapeau se déplacer sur la paire correspondante.
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
		active = "aucun"
	}
	return fmt.Sprintf("actif=%s | disponibles: %s", active, strings.Join(avail, ", "))
}

var keyGestures = map[byte]string{
	0x01: "clic G", 0x02: "clic D", 0x03: "double clic G", 0x04: "double clic D",
	0x05: "triple clic G", 0x06: "triple clic D", 0x07: "appui long G", 0x08: "appui long D",
}

var keyActions = map[byte]string{
	0x01: "volume +", 0x02: "volume −", 0x03: "lecture/pause", 0x04: "mode jeu",
	0x05: "piste préc.", 0x06: "piste suiv.", 0x07: "mode ANC", 0x08: "assistant vocal",
}

// describe donne une lecture humaine du payload quand on sait l'interpréter.
func describe(f frame) string {
	if f.Command[0]&0x80 == 0 {
		return ""
	}
	base := [2]byte{f.Command[0] &^ 0x80, f.Command[1]}
	p := f.Payload
	if len(p) == 0 {
		return ""
	}
	// Confirmé sur le fil : les payloads scalaires font 2 octets big-endian.
	// La valeur utile est dans le dernier octet.
	v := p[len(p)-1]

	switch base {
	case [2]byte{0x03, 0x06}, [2]byte{0x03, 0x07}, [2]byte{0x03, 0x23}:
		if v <= 100 {
			return fmt.Sprintf("%d %%", v)
		}
		return fmt.Sprintf("hors plage batterie (%d)", v)

	case [2]byte{0x03, 0x09}:
		// 1er octet de statut, puis version en ASCII.
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
		// Octet de tête puis paires [geste, action].
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
			return "désactivé"
		case 0x01:
			return "activé"
		}
	}
	return ""
}

func parseCmd(s string) ([2]byte, error) {
	var c [2]byte
	clean := strings.ReplaceAll(strings.ReplaceAll(s, " ", ""), "0x", "")
	b, err := hex.DecodeString(clean)
	if err != nil {
		return c, fmt.Errorf("commande hex invalide %q: %w", s, err)
	}
	if len(b) != 2 {
		return c, fmt.Errorf("la commande doit faire 2 octets, reçu %d", len(b))
	}
	return [2]byte{b[0], b[1]}, nil
}

// readAvailable draine le socket pendant la fenêtre donnée.
func readAvailable(fd int, window time.Duration, acc []byte) ([]frame, []byte, []byte) {
	deadline := time.Now().Add(window)
	buf := make([]byte, 1024)
	var raw []byte
	var all []frame

	for time.Now().Before(deadline) {
		n, err := syscall.Read(fd, buf)
		if err != nil {
			// EINTR : le runtime Go interrompt les syscalls bloquants pour la
			// préemption — il faut simplement réessayer.
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
			break // réponse obtenue, inutile d'attendre la fenêtre complète
		}
	}
	return all, acc, raw
}

// exchange envoie une commande et attend une réponse, avec retry.
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
			fmt.Fprintln(os.Stderr, "  échec d'écriture:", err)
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
			fmt.Printf("  (essai %d sans réponse, nouvelle tentative)\n", try)
		}
	}
	return nil, lastRaw
}

// scanChannels cherche les canaux RFCOMM qui acceptent une connexion.
func scanChannels(addr [6]byte, max int, timeout time.Duration) []uint8 {
	var found []uint8
	for ch := 1; ch <= max; ch++ {
		fd, err := connectRFCOMM(addr, uint8(ch), timeout)
		if err == nil {
			found = append(found, uint8(ch))
			syscall.Close(fd)
			fmt.Printf("  canal %2d : ouvert\n", ch)
		} else if !errors.Is(err, syscall.ECONNREFUSED) {
			fmt.Printf("  canal %2d : %v\n", ch, err)
		}
	}
	return found
}

func main() {
	var (
		addrStr    = flag.String("addr", "98:80:BB:40:D4:ED", "adresse MAC des écouteurs")
		channel    = flag.Int("channel", 0, "canal RFCOMM (0 = découverte automatique)")
		cmdStr     = flag.String("cmd", "0306", "commande(s) 2 octets hex, séparées par des virgules")
		payloadStr = flag.String("payload", "", "payload hex (appliqué à toutes les commandes)")
		eqVendor   = flag.Bool("eq-vendor", false, "utiliser le vendorId EQ 001D au lieu de 000A")
		allowWrite = flag.Bool("allow-write", false, "autoriser les commandes hors liste blanche")
		scan       = flag.Bool("scan", false, "seulement scanner les canaux RFCOMM")
		maxChan    = flag.Int("max-channel", 30, "canal maximum exploré")
		window     = flag.Duration("window", 1500*time.Millisecond, "fenêtre d'attente par essai")
		retries    = flag.Int("retry", 3, "nombre d'essais par commande")
		settle     = flag.Duration("settle", 600*time.Millisecond, "délai après connexion avant émission")
		gap        = flag.Duration("gap", 250*time.Millisecond, "délai entre deux commandes")
		connTO     = flag.Duration("connect-timeout", 10*time.Second, "timeout de connexion")
		quiet      = flag.Bool("quiet", false, "sortie compacte (une ligne par commande)")
		listen     = flag.Duration("listen", 0, "après les commandes, écouter les trames spontanées pendant cette durée")
	)
	flag.Parse()

	addr, err := parseMAC(*addrStr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "erreur:", err)
		os.Exit(2)
	}

	// --- Découverte du canal ---
	ch := uint8(*channel)
	if *scan || ch == 0 {
		fmt.Printf("Découverte des canaux RFCOMM sur %s (1..%d)…\n", *addrStr, *maxChan)
		found := scanChannels(addr, *maxChan, *connTO)
		if len(found) == 0 {
			fmt.Fprintln(os.Stderr, "\nAucun canal RFCOMM ouvert.")
			fmt.Fprintln(os.Stderr, "Les écouteurs sont-ils allumés et hors de l'étui ?")
			os.Exit(1)
		}
		fmt.Printf("Canaux ouverts : %v\n", found)
		if *scan {
			return
		}
		ch = found[0]
	}

	// --- Commandes + garde-fou lecture seule ---
	var cmds [][2]byte
	var labels []string
	for _, s := range strings.Split(*cmdStr, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		c, err := parseCmd(s)
		if err != nil {
			fmt.Fprintln(os.Stderr, "erreur:", err)
			os.Exit(2)
		}
		label, known := readOnlyCmds[c]
		if !known {
			if !*allowWrite {
				fmt.Fprintf(os.Stderr,
					"Refus : %02X%02X n'est pas une commande de lecture connue.\n"+
						"Cette sonde est en lecture seule. Utiliser -allow-write pour forcer.\n",
					c[0], c[1])
				os.Exit(2)
			}
			label = "hors liste blanche"
		}
		cmds = append(cmds, c)
		labels = append(labels, label)
	}
	if len(cmds) == 0 {
		fmt.Fprintln(os.Stderr, "aucune commande à envoyer")
		os.Exit(2)
	}

	var payload []byte
	if *payloadStr != "" {
		payload, err = hex.DecodeString(strings.ReplaceAll(*payloadStr, " ", ""))
		if err != nil {
			fmt.Fprintln(os.Stderr, "payload hex invalide:", err)
			os.Exit(2)
		}
	}

	vendor := vendorDefault
	if *eqVendor {
		vendor = vendorEQ
	}

	// --- Connexion (une seule, réutilisée) ---
	fd, err := connectRFCOMM(addr, ch, *connTO)
	if err != nil {
		fmt.Fprintln(os.Stderr, "échec de connexion:", err)
		os.Exit(1)
	}
	defer syscall.Close(fd)
	fmt.Printf("Connecté sur le canal %d.\n", ch)

	tv := syscall.NsecToTimeval(int64(300 * time.Millisecond))
	if err := syscall.SetsockoptTimeval(fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &tv); err != nil {
		fmt.Fprintln(os.Stderr, "avertissement: SO_RCVTIMEO non appliqué:", err)
	}

	// Laisser le lien se stabiliser : sans ce délai les premières commandes
	// restent fréquemment sans réponse.
	time.Sleep(*settle)

	ok := 0
	for i, c := range cmds {
		if i > 0 {
			time.Sleep(*gap)
		}
		if !*quiet {
			fmt.Printf("\n=== %02X%02X — %s ===\n", c[0], c[1], labels[i])
		}

		// Les écritures ne sont JAMAIS réessayées : plusieurs commandes de type
		// `set` prennent effet sans acquitter (mode ANC, multipoint). Un retry
		// rejouerait l'effet de bord — observé sur le H3 avec trois invites
		// vocales « passthrough » pour une seule écriture voulue.
		// Pour vérifier qu'un `set` a pris, relire l'état, ne pas renvoyer.
		tries := *retries
		if _, isRead := readOnlyCmds[c]; !isRead {
			tries = 1
		}
		frames, raw := exchange(fd, c, payload, vendor, *window, tries, !*quiet)

		if len(frames) == 0 {
			if *quiet {
				fmt.Printf("%02X%02X  %-28s  SANS RÉPONSE\n", c[0], c[1], labels[i])
			} else {
				fmt.Printf("  SANS RÉPONSE après %d essais\n", *retries)
				if len(raw) > 0 {
					fmt.Printf("  octets non décodés : %s\n",
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
					fmt.Printf("    ⚠ en-tête inattendu (sof=%02X version=%02X)\n",
						f.SOF, f.Version)
				}
			}
		}
	}

	fmt.Printf("\n--- %d/%d commande(s) avec réponse ---\n", ok, len(cmds))

	if *listen > 0 {
		fmt.Printf("\n=== écoute des trames spontanées pendant %s ===\n", *listen)
		fmt.Println("(touchez les écouteurs maintenant)")
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
				fmt.Fprintln(os.Stderr, "lecture:", err)
				break
			}
			if r == 0 {
				fmt.Fprintln(os.Stderr, "connexion fermée par le périphérique")
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
		fmt.Printf("\n--- %d trame(s) spontanée(s) ---\n", n)
		if n == 0 {
			fmt.Println("Aucune trame : les écouteurs ne rapportent pas les appuis sur ce canal.")
		}
		return
	}

	if ok == 0 {
		os.Exit(1)
	}
}
