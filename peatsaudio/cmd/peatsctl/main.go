// peatsctl — outil en ligne de commande pour vérifier le transport RFCOMM.
//
// Sert surtout à valider une plateforme dont le transport n'a jamais été
// exécuté : il exerce la connexion, la lecture et, si on le demande, une
// écriture, sans dépendre de l'interface graphique.
//
//	peatsctl -addr 98:80:BB:40:D4:ED
//	peatsctl -addr ... -anc 1
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"peatsaudio/internal/peats"
)

func main() {
	addr := flag.String("addr", "", "adresse MAC des écouteurs (obligatoire)")
	channel := flag.Int("channel", 0, "canal RFCOMM (0 = découverte automatique)")
	anc := flag.Int("anc", -1, "écrire le mode ANC : 0 arrêt, 1 ANC, 2 transparence")
	flag.Parse()

	if strings.TrimSpace(*addr) == "" {
		fmt.Fprintln(os.Stderr, "Indiquez l'adresse des écouteurs avec -addr.")
		fmt.Fprintln(os.Stderr, "Sous Windows : Paramètres > Bluetooth > votre appareil.")
		flag.Usage()
		os.Exit(2)
	}

	fmt.Printf("Connexion à %s…\n", *addr)
	c, err := peats.Connect(*addr, uint8(*channel))
	if err != nil {
		fmt.Fprintln(os.Stderr, "Échec :", explain(err))
		os.Exit(1)
	}
	defer c.Close()
	fmt.Printf("Connecté sur le canal RFCOMM %d.\n\n", c.Channel())

	reads := []struct {
		label string
		cmd   peats.Command
		show  func(peats.Frame) string
	}{
		{"batterie gauche", peats.CmdBatteryLeft, pct},
		{"batterie droite", peats.CmdBatteryRight, pct},
		{"batterie étui", peats.CmdBatteryCase, pct},
		{"firmware", peats.CmdFirmware, func(f peats.Frame) string {
			if len(f.Payload) > 1 {
				return strings.TrimRight(string(f.Payload[1:]), "\x00")
			}
			return "?"
		}},
		{"mode ANC", peats.CmdANCModeGet, ancMode},
		{"sous-mode ANC", peats.CmdANCLevelGet, active},
		{"LDAC", peats.CmdLDACGet, onOff},
		{"multipoint", peats.CmdMultipointGet, onOff},
		{"mode jeu", peats.CmdGameModeGet, onOff},
	}

	ok := 0
	for _, r := range reads {
		f, err := c.Get(r.cmd)
		if err != nil {
			fmt.Printf("  %-18s %s  — pas de réponse\n", r.label, r.cmd)
			continue
		}
		ok++
		fmt.Printf("  %-18s %s  %-22X %s\n", r.label, r.cmd, f.Payload, r.show(f))
	}
	fmt.Printf("\n%d/%d lectures abouties.\n", ok, len(reads))

	if *anc >= 0 {
		fmt.Printf("\nÉcriture du mode ANC = %d…\n", *anc)
		// Pas de réémission : plusieurs `set` prennent effet sans acquitter.
		c.Set(peats.CmdANCModeSet, peats.CmdANCModeGet, []byte{byte(*anc)})
		if f, err := c.Get(peats.CmdANCModeGet); err == nil {
			fmt.Printf("Relecture : %s\n", ancMode(f))
		} else {
			fmt.Println("Relecture impossible :", err)
		}
	}

	if ok == 0 {
		fmt.Fprintln(os.Stderr, "\nAucune lecture n'a abouti : le transport ne fonctionne pas.")
		os.Exit(1)
	}
}

func explain(err error) string {
	switch {
	case errors.Is(err, peats.ErrBusy):
		return "canal de contrôle occupé par un autre appareil — si le multipoint " +
			"est actif, coupez le Bluetooth sur l'autre appareil"
	case errors.Is(err, peats.ErrNoChannel):
		return "aucun canal RFCOMM — écouteurs éteints, dans l'étui, ou non appairés"
	case errors.Is(err, peats.ErrUnsupported):
		return "transport RFCOMM non implémenté sur cette plateforme"
	default:
		return err.Error()
	}
}

func pct(f peats.Frame) string { return fmt.Sprintf("%d %%", f.Scalar()) }

func onOff(f peats.Frame) string {
	if f.Scalar() == 1 {
		return "activé"
	}
	return "désactivé"
}

func ancMode(f peats.Frame) string {
	switch f.Scalar() {
	case 0:
		return "arrêt"
	case 1:
		return "réduction de bruit"
	case 2:
		return "transparence"
	case 3:
		return "adaptatif"
	default:
		return fmt.Sprintf("0x%02X", f.Scalar())
	}
}

func active(f peats.Frame) string {
	code, ok := f.ActiveCode()
	if !ok {
		return "aucun actif"
	}
	names := map[byte]string{
		peats.ANCLevelAdaptive: "adaptatif",
		peats.ANCLevelIndoor:   "intérieur",
		peats.ANCLevelOutdoor:  "extérieur",
		peats.ANCLevelTraffic:  "trafic",
	}
	if n, hit := names[code]; hit {
		return n
	}
	return fmt.Sprintf("0x%02X", code)
}
