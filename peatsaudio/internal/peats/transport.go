package peats

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Le protocole PeatsAudio circule sur RFCOMM (Bluetooth Classic, profil SPP).
// Chaque système expose ce transport via une API différente ; seule cette
// couche change d'une plateforme à l'autre.
//
//	Linux   AF_BLUETOOTH / BTPROTO_RFCOMM   (transport_linux.go)
//	Windows AF_BTH / BTHPROTO_RFCOMM        (transport_windows.go)
//	macOS   IOBluetooth                     (transport_darwin.go)
//
// iOS est hors d'atteinte : le profil SPP y est réservé aux accessoires
// certifiés MFi, ce que ces écouteurs ne sont pas.

// rfcommConn est une connexion RFCOMM établie.
type rfcommConn interface {
	Read(p []byte) (int, error)
	Write(p []byte) (int, error)
	// SetReadTimeout borne l'attente d'un Read. Un dépassement doit renvoyer
	// une erreur satisfaisant isTimeout.
	SetReadTimeout(d time.Duration) error
	Close() error
}

// dialRFCOMM ouvre une connexion vers addr sur le canal donné.
// Implémenté par fichier selon la plateforme.
//
// Doit renvoyer une erreur satisfaisant errors.Is(err, ErrBusy) quand le canal
// est déjà tenu par un autre appareil, et errors.Is(err, ErrRefused) quand
// aucun service n'écoute sur ce canal.
var dialRFCOMM func(addr [6]byte, channel uint8, timeout time.Duration) (rfcommConn, error)

// maxChannel borne le balayage de découverte. Les canaux RFCOMM vont de 1 à 30.
const maxChannel = 30

var (
	// ErrBusy : le canal de contrôle est monopolisé par un autre appareil.
	// Typiquement multipoint actif avec un téléphone connecté.
	ErrBusy = errors.New("canal de contrôle occupé par un autre appareil")

	// ErrRefused : aucun service n'écoute sur ce canal.
	ErrRefused = errors.New("canal refusé")

	// ErrUnreachable : l'appareil est appairé mais injoignable — éteint, dans
	// son étui, ou hors de portée. Distinct d'ErrRefused : ce n'est pas le
	// canal qui est fermé, c'est l'appareil qui n'est pas là. Aucun canal ne
	// répondra, le balayage est donc inutile.
	ErrUnreachable = errors.New("écouteurs injoignables")

	// ErrNoChannel : aucun canal RFCOMM ouvert.
	ErrNoChannel = errors.New("aucun canal RFCOMM disponible")

	ErrTimeout      = errors.New("pas de réponse des écouteurs")
	ErrNotConnected = errors.New("non connecté")

	// ErrUnsupported : plateforme sans transport RFCOMM implémenté.
	ErrUnsupported = errors.New("transport RFCOMM non disponible sur cette plateforme")
)

// ParseMAC convertit "98:80:BB:40:D4:ED" en tableau d'octets, du plus
// significatif au moins significatif.
//
// Attention : les API de chaque plateforme attendent un ordre différent.
// Linux veut l'ordre inverse (little-endian), Windows un entier 64 bits.
// La conversion est faite dans chaque transport, pas ici.
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
		out[i] = byte(v)
	}
	return out, nil
}

// dialAny balaie les canaux RFCOMM et renvoie la PREMIÈRE connexion établie.
//
// sdptool ne renvoie rien d'exploitable sur ces écouteurs, d'où le balayage.
// La connexion est délibérément conservée plutôt que refermée puis rouverte :
// les écouteurs refusent une reconnexion immédiate sur le même canal tant que
// la session RFCOMM précédente n'est pas démontée.
func dialAny(addr [6]byte) (rfcommConn, uint8, error) {
	if dialRFCOMM == nil {
		return nil, 0, ErrUnsupported
	}
	sawBusy := false
	for ch := uint8(1); ch <= maxChannel; ch++ {
		c, err := dialRFCOMM(addr, ch, 3*time.Second)
		if err == nil {
			return c, ch, nil
		}
		// L'appareil lui-même est absent : les 29 canaux restants échoueront
		// de la même façon. On remonte la cause réelle au lieu de la diluer
		// en « aucun canal disponible ».
		if errors.Is(err, ErrUnreachable) {
			return nil, 0, err
		}
		if errors.Is(err, ErrBusy) {
			sawBusy = true
		}
	}
	if sawBusy {
		return nil, 0, ErrBusy
	}
	return nil, 0, ErrNoChannel
}
