package main

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"peatsaudio/internal/peats"
)

// DefaultMAC : adresse des écouteurs de développement. Modifiable dans l'UI.
const DefaultMAC = "98:80:BB:40:D4:ED"

// State est l'état complet du périphérique, poussé vers le frontend.
type State struct {
	Connected bool   `json:"connected"`
	MAC       string `json:"mac"`
	Channel   int    `json:"channel"`
	Firmware  string `json:"firmware"`

	BatteryLeft  int `json:"batteryLeft"`
	BatteryRight int `json:"batteryRight"`
	BatteryCase  int `json:"batteryCase"`

	ANCMode   int          `json:"ancMode"`
	ANCLevel  int          `json:"ancLevel"`
	ANCLevels []peats.Pair `json:"ancLevels"`

	TransparencyLevel  int          `json:"transparencyLevel"`
	TransparencyLevels []peats.Pair `json:"transparencyLevels"`

	GameMode      bool `json:"gameMode"`
	Multipoint    bool `json:"multipoint"`
	LDAC          bool `json:"ldac"`
	LEAudio       bool `json:"leAudio"`
	TouchDisabled bool `json:"touchDisabled"`

	PromptVolume int `json:"promptVolume"`
	VoicePack    int `json:"voicePack"`

	KeyMappings []peats.KeyMapping `json:"keyMappings"`

	// Supported indique quelles fonctions ce modèle expose réellement.
	// Renseigné au rafraîchissement : une commande sans réponse répétée est
	// considérée absente du matériel.
	Supported map[string]bool `json:"supported"`

	BatteryAlert  bool `json:"batteryAlert"`
	BatteryThresh int  `json:"batteryThresh"`
}

// App est le contexte applicatif exposé à Wails.
type App struct {
	ctx context.Context

	mu     sync.RWMutex
	client *peats.Client
	state  State

	// notified retient quelles sources ont déjà déclenché une alerte, pour ne
	// pas répéter la notification à chaque sondage.
	notified map[string]bool

	monitorStop chan struct{}
}

func NewApp() *App {
	return &App{
		state: State{
			MAC:           DefaultMAC,
			Supported:     map[string]bool{},
			BatteryAlert:  true,
			BatteryThresh: 20,
		},
		notified: map[string]bool{},
	}
}

func (a *App) startup(ctx context.Context) { a.ctx = ctx }

func (a *App) shutdown(context.Context) {
	a.stopMonitor()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.client != nil {
		a.client.Close()
		a.client = nil
	}
}

func (a *App) emit(event string, data ...any) {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, event, data...)
	}
}

// log pousse une ligne dans la console de l'UI.
func (a *App) log(format string, args ...any) {
	a.emit("log", fmt.Sprintf(format, args...))
}

func (a *App) pushState() {
	a.mu.RLock()
	s := a.state
	a.mu.RUnlock()
	a.emit("state", s)
}

// GetState renvoie l'état courant (appel initial du frontend).
func (a *App) GetState() State {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.state
}

// ---------------------------------------------------------------- connexion

// Connect ouvre la connexion de contrôle et fait un premier rafraîchissement.
func (a *App) Connect(mac string) string {
	if strings.TrimSpace(mac) == "" {
		mac = DefaultMAC
	}

	a.mu.Lock()
	if a.client != nil {
		a.client.Close()
		a.client = nil
	}
	a.mu.Unlock()

	a.log("Connexion à %s…", mac)
	cl, err := peats.Connect(mac, 0)
	if err != nil {
		msg := describeConnectError(err)
		a.log("Échec : %s", msg)
		a.mu.Lock()
		a.state.Connected = false
		a.mu.Unlock()
		a.pushState()
		return msg
	}

	// Toutes les trames sont diffusées vers le journal de l'UI.
	cl.OnFrame = func(f peats.Frame) {
		a.emit("frame", map[string]any{
			"cmd":     f.Command.String(),
			"payload": strings.ToUpper(hex.EncodeToString(f.Payload)),
			"ts":      time.Now().Format("15:04:05.000"),
		})
	}

	a.mu.Lock()
	a.client = cl
	a.state.Connected = true
	a.state.MAC = mac
	a.state.Channel = int(cl.Channel())
	a.notified = map[string]bool{}
	a.mu.Unlock()

	a.log("Connecté sur le canal RFCOMM %d.", cl.Channel())
	a.Refresh()
	a.startMonitor()
	return ""
}

func describeConnectError(err error) string {
	switch {
	case errors.Is(err, peats.ErrBusy):
		return "Canal de contrôle occupé. Si le multipoint est actif, un autre " +
			"appareil (téléphone avec l'app PeatsAudio ouverte) le monopolise — " +
			"coupez-y le Bluetooth puis réessayez."
	case errors.Is(err, peats.ErrNoChannel):
		return "Aucun canal RFCOMM. Écouteurs éteints, dans l'étui, ou non appairés."
	default:
		return err.Error()
	}
}

func (a *App) Disconnect() {
	a.stopMonitor()
	a.mu.Lock()
	if a.client != nil {
		a.client.Close()
		a.client = nil
	}
	a.state.Connected = false
	a.mu.Unlock()
	a.log("Déconnecté.")
	a.pushState()
}

// ------------------------------------------------- surveillance des batteries

const batteryPollInterval = 90 * time.Second

func (a *App) startMonitor() {
	a.stopMonitor()

	stop := make(chan struct{})
	a.mu.Lock()
	a.monitorStop = stop
	a.mu.Unlock()

	go func() {
		t := time.NewTicker(batteryPollInterval)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				a.pollBatteries()
			}
		}
	}()
}

func (a *App) stopMonitor() {
	a.mu.Lock()
	if a.monitorStop != nil {
		close(a.monitorStop)
		a.monitorStop = nil
	}
	a.mu.Unlock()
}

// pollBatteries relit les niveaux et déclenche une alerte au franchissement du
// seuil. Une hystérésis de 5 points évite le clignotement autour du seuil.
func (a *App) pollBatteries() {
	c := a.conn()
	if c == nil {
		return
	}

	readings := []struct {
		key, label string
		cmd        peats.Command
		set        func(int)
	}{
		{"left", "écouteur gauche", peats.CmdBatteryLeft,
			func(v int) { a.state.BatteryLeft = v }},
		{"right", "écouteur droit", peats.CmdBatteryRight,
			func(v int) { a.state.BatteryRight = v }},
		{"case", "boîtier", peats.CmdBatteryCase,
			func(v int) { a.state.BatteryCase = v }},
	}

	a.mu.RLock()
	alert, thresh := a.state.BatteryAlert, a.state.BatteryThresh
	a.mu.RUnlock()

	changed := false
	for _, r := range readings {
		f, err := c.Get(r.cmd)
		if err != nil {
			continue
		}
		v := int(f.Scalar())
		if v > 100 { // valeur aberrante : on ignore plutôt que d'alerter à tort
			continue
		}
		a.mu.Lock()
		r.set(v)
		already := a.notified[r.key]
		switch {
		case v <= thresh && !already:
			a.notified[r.key] = true
			a.mu.Unlock()
			if alert {
				a.notifyLowBattery(r.label, v)
			}
		case v > thresh+5 && already:
			a.notified[r.key] = false
			a.mu.Unlock()
		default:
			a.mu.Unlock()
		}
		changed = true
	}

	if changed {
		a.pushState()
	}
}

// notifyLowBattery envoie une notification de bureau et prévient l'UI.
func (a *App) notifyLowBattery(label string, pct int) {
	title := "Batterie faible — SoundPEATS"
	body := fmt.Sprintf("%s à %d %%.", label, pct)

	a.log("⚠ %s", body)
	a.emit("alert", map[string]any{"label": label, "pct": pct})

	// notify-send est présent sur toute installation de bureau freedesktop.
	// Son absence n'est pas une erreur bloquante : l'alerte reste visible
	// dans l'application.
	cmd := exec.Command("notify-send",
		"--app-name=PeatsAudio",
		"--urgency=critical",
		"--icon=audio-headset",
		title, body)
	if err := cmd.Run(); err != nil {
		a.log("Notification système indisponible (%v).", err)
	}
}

// SetBatteryAlert active ou coupe les notifications de batterie faible.
func (a *App) SetBatteryAlert(on bool) {
	a.mu.Lock()
	a.state.BatteryAlert = on
	a.mu.Unlock()
	a.pushState()
}

// SetBatteryThreshold règle le seuil d'alerte (5–50 %).
func (a *App) SetBatteryThreshold(v int) {
	if v < 5 {
		v = 5
	}
	if v > 50 {
		v = 50
	}
	a.mu.Lock()
	a.state.BatteryThresh = v
	a.notified = map[string]bool{} // le seuil change : on réarme les alertes
	a.mu.Unlock()
	a.pushState()
}

// TestBatteryNotification envoie une notification factice pour vérifier que le
// canal de notification du bureau fonctionne.
func (a *App) TestBatteryNotification() {
	a.notifyLowBattery("test", 15)
}

func (a *App) conn() *peats.Client {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.client
}

// ---------------------------------------------------------------- lectures

// Refresh relit tout l'état du périphérique et met à jour la carte de support.
func (a *App) Refresh() {
	c := a.conn()
	if c == nil {
		return
	}

	supported := map[string]bool{}

	getScalar := func(key string, cmd peats.Command) (byte, bool) {
		f, err := c.Get(cmd)
		if err != nil {
			supported[key] = false
			return 0, false
		}
		supported[key] = true
		return f.Scalar(), true
	}
	getBool := func(key string, cmd peats.Command) (bool, bool) {
		v, ok := getScalar(key, cmd)
		return v == 1, ok
	}

	a.mu.Lock()
	s := &a.state

	if v, ok := getScalar("batteryLeft", peats.CmdBatteryLeft); ok {
		s.BatteryLeft = int(v)
	}
	if v, ok := getScalar("batteryRight", peats.CmdBatteryRight); ok {
		s.BatteryRight = int(v)
	}
	if v, ok := getScalar("batteryCase", peats.CmdBatteryCase); ok {
		s.BatteryCase = int(v)
	}

	if f, err := c.Get(peats.CmdFirmware); err == nil {
		supported["firmware"] = true
		// 1er octet de statut, puis la version en ASCII.
		if len(f.Payload) > 1 {
			s.Firmware = strings.TrimRight(string(f.Payload[1:]), "\x00")
		}
	} else {
		supported["firmware"] = false
	}

	if v, ok := getScalar("ancMode", peats.CmdANCModeGet); ok {
		s.ANCMode = int(v)
	}

	if f, err := c.Get(peats.CmdANCLevelGet); err == nil {
		supported["ancLevel"] = true
		s.ANCLevels = f.Pairs()
		if code, ok := f.ActiveCode(); ok {
			s.ANCLevel = int(code)
		}
	} else {
		supported["ancLevel"] = false
	}

	if f, err := c.Get(peats.CmdTransparentLevelGet); err == nil {
		supported["transparencyLevel"] = true
		s.TransparencyLevels = f.Pairs()
		if code, ok := f.ActiveCode(); ok {
			s.TransparencyLevel = int(code)
		}
	} else {
		supported["transparencyLevel"] = false
	}

	if v, ok := getBool("gameMode", peats.CmdGameModeGet); ok {
		s.GameMode = v
	}
	if v, ok := getBool("multipoint", peats.CmdMultipointGet); ok {
		s.Multipoint = v
	}
	if v, ok := getBool("ldac", peats.CmdLDACGet); ok {
		s.LDAC = v
	}
	if v, ok := getBool("leAudio", peats.CmdLEAudioGet); ok {
		s.LEAudio = v
	}
	if v, ok := getBool("touchDisabled", peats.CmdTouchDisableGet); ok {
		s.TouchDisabled = v
	}
	if v, ok := getScalar("promptVolume", peats.CmdPromptVolumeGet); ok {
		s.PromptVolume = int(v)
	}
	if v, ok := getScalar("voicePack", peats.CmdVoicePackGet); ok {
		s.VoicePack = int(v)
	}

	if f, err := c.Get(peats.CmdCustomKeyGet); err == nil {
		supported["keyMappings"] = true
		s.KeyMappings = f.KeyMappings()
	} else {
		supported["keyMappings"] = false
	}

	s.Supported = supported
	a.mu.Unlock()

	n := 0
	for _, ok := range supported {
		if ok {
			n++
		}
	}
	a.log("Rafraîchi : %d/%d fonctions disponibles sur ce modèle.", n, len(supported))
	a.pushState()
}

// ---------------------------------------------------------------- écritures
//
// Après chaque écriture on relit l'état : plusieurs `set` prennent effet sans
// acquitter, et réémettre la commande rejouerait l'effet de bord.

func (a *App) settle() { time.Sleep(400 * time.Millisecond) }

func (a *App) SetANCMode(mode int) {
	c := a.conn()
	if c == nil {
		return
	}
	c.Set(peats.CmdANCModeSet, peats.CmdANCModeGet, []byte{byte(mode)})
	a.settle()
	if f, err := c.Get(peats.CmdANCModeGet); err == nil {
		a.mu.Lock()
		a.state.ANCMode = int(f.Scalar())
		a.mu.Unlock()
	}
	a.pushState()
}

func (a *App) SetANCLevel(code int) {
	c := a.conn()
	if c == nil {
		return
	}
	c.Set(peats.CmdANCLevelSet, peats.CmdANCLevelGet, []byte{byte(code)})
	a.settle()
	if f, err := c.Get(peats.CmdANCLevelGet); err == nil {
		a.mu.Lock()
		a.state.ANCLevels = f.Pairs()
		if ac, ok := f.ActiveCode(); ok {
			a.state.ANCLevel = int(ac)
		}
		a.mu.Unlock()
	}
	a.pushState()
}

func (a *App) SetTransparencyLevel(code int) {
	c := a.conn()
	if c == nil {
		return
	}
	c.Set(peats.CmdTransparentLevelSet, peats.CmdTransparentLevelGet, []byte{byte(code)})
	a.settle()
	if f, err := c.Get(peats.CmdTransparentLevelGet); err == nil {
		a.mu.Lock()
		a.state.TransparencyLevels = f.Pairs()
		if ac, ok := f.ActiveCode(); ok {
			a.state.TransparencyLevel = int(ac)
		}
		a.mu.Unlock()
	}
	a.pushState()
}

// setToggle factorise les bascules : écrire, laisser retomber, relire.
func (a *App) setToggle(setCmd, getCmd peats.Command, on bool, apply func(bool)) {
	c := a.conn()
	if c == nil {
		return
	}
	c.SetBool(setCmd, getCmd, on)
	a.settle()
	if f, err := c.Get(getCmd); err == nil {
		a.mu.Lock()
		apply(f.Scalar() == 1)
		a.mu.Unlock()
	}
	a.pushState()
}

func (a *App) SetGameMode(on bool) {
	a.setToggle(peats.CmdGameModeSet, peats.CmdGameModeGet, on,
		func(v bool) { a.state.GameMode = v })
}

func (a *App) SetLEAudio(on bool) {
	a.setToggle(peats.CmdLEAudioSet, peats.CmdLEAudioGet, on,
		func(v bool) { a.state.LEAudio = v })
}

func (a *App) SetTouchDisabled(on bool) {
	a.setToggle(peats.CmdTouchDisableSet, peats.CmdTouchDisableGet, on,
		func(v bool) { a.state.TouchDisabled = v })
}

func (a *App) SetVoicePack(code int) {
	c := a.conn()
	if c == nil {
		return
	}
	c.Set(peats.CmdVoicePackSet, peats.CmdVoicePackGet, []byte{byte(code)})
	a.settle()
	if f, err := c.Get(peats.CmdVoicePackGet); err == nil {
		a.mu.Lock()
		a.state.VoicePack = int(f.Scalar())
		a.mu.Unlock()
	}
	a.pushState()
}

func (a *App) SetPromptVolume(v int) {
	c := a.conn()
	if c == nil {
		return
	}
	c.Set(peats.CmdPromptVolumeSet, peats.CmdPromptVolumeGet, []byte{byte(v)})
	a.settle()
	if f, err := c.Get(peats.CmdPromptVolumeGet); err == nil {
		a.mu.Lock()
		a.state.PromptVolume = int(f.Scalar())
		a.mu.Unlock()
	}
	a.pushState()
}

// SetMultipoint bascule le multipoint.
//
// Le firmware désactive LDAC de lui-même quand le multipoint s'active : la
// bande passante ne permet pas du LDAC sur deux liens. On relit donc les deux.
// L'opération reconfigure les liens et peut rendre le canal indisponible un
// court instant.
func (a *App) SetMultipoint(on bool) {
	c := a.conn()
	if c == nil {
		return
	}
	if on {
		a.log("Activation du multipoint — LDAC va être désactivé par le firmware " +
			"(bande passante insuffisante sur deux liens).")
	}
	c.SetBool(peats.CmdMultipointSet, peats.CmdMultipointGet, on)
	time.Sleep(1200 * time.Millisecond) // reconfiguration des liens

	if f, err := c.Get(peats.CmdMultipointGet); err == nil {
		a.mu.Lock()
		a.state.Multipoint = f.Scalar() == 1
		a.mu.Unlock()
	}
	if f, err := c.Get(peats.CmdLDACGet); err == nil {
		a.mu.Lock()
		a.state.LDAC = f.Scalar() == 1
		a.mu.Unlock()
	}
	a.pushState()
}

// SetLDAC bascule LDAC, exclusif du multipoint (voir SetMultipoint).
func (a *App) SetLDAC(on bool) {
	c := a.conn()
	if c == nil {
		return
	}
	if on {
		a.log("Activation de LDAC — le multipoint sera désactivé (exclusifs).")
	}
	c.SetBool(peats.CmdLDACSet, peats.CmdLDACGet, on)
	time.Sleep(1200 * time.Millisecond)

	if f, err := c.Get(peats.CmdLDACGet); err == nil {
		a.mu.Lock()
		a.state.LDAC = f.Scalar() == 1
		a.mu.Unlock()
	}
	if f, err := c.Get(peats.CmdMultipointGet); err == nil {
		a.mu.Lock()
		a.state.Multipoint = f.Scalar() == 1
		a.mu.Unlock()
	}
	a.pushState()
}

// ---------------------------------------------------------------- tactile

// SetKeyMappings réécrit le mapping tactile.
//
// ⚠ NON VÉRIFIÉ sur le fil. La structure du payload reproduit celle observée
// en lecture, mais aucune écriture n'a été validée. L'UI le signale.
func (a *App) SetKeyMappings(ms []peats.KeyMapping) {
	c := a.conn()
	if c == nil {
		return
	}
	a.log("Écriture du mapping tactile (non vérifiée sur ce matériel).")
	c.Set(peats.CmdCustomKeySet, peats.CmdCustomKeyGet, peats.EncodeKeyMappings(ms))
	a.settle()
	if f, err := c.Get(peats.CmdCustomKeyGet); err == nil {
		a.mu.Lock()
		a.state.KeyMappings = f.KeyMappings()
		a.mu.Unlock()
	}
	a.pushState()
}

// Le mode démo tactile (CUSTOMIZE_TOUCH_DEBUG / SET_CUSTOMIZE_TOUCH_DEMO)
// présent dans l'app Android a été testé sur le H3 : la commande 03 AB 00 00
// est bien acquittée (83 AB 00 00 01), mais AUCUNE trame n'est émise lors des
// appuis, et ceux-ci continuent d'exécuter leur action normale. Le tactile est
// géré en local par le firmware sur ce modèle. Fonctionnalité retirée plutôt
// que d'exposer un bouton sans effet.

// ---------------------------------------------------------------- égaliseur

// ApplyEQ applique une courbe paramétrique.
//
// Le firmware décide de la version de protocole EQ : v1 pour les puces
// Qualcomm et une liste de firmwares en dur, v2/v3 sinon. Le H3 (firmware
// « S5 ») utilise v2/v3.
func (a *App) ApplyEQ(bands []peats.EQBand, masterGain float64) string {
	c := a.conn()
	if c == nil {
		return "non connecté"
	}
	if err := c.ApplyEQ(bands, masterGain, a.eqUsesV1()); err != nil {
		a.log("EQ : %v", err)
		return err.Error()
	}
	a.log("EQ appliqué : %d bande(s), gain maître %.1f dB.", len(bands), masterGain)
	return ""
}

// eqUsesV1 reproduit needInParallel() de l'app Android.
func (a *App) eqUsesV1() bool {
	a.mu.RLock()
	fw := a.state.Firmware
	a.mu.RUnlock()

	for _, code := range []string{
		"CAPSULE3_PROV2", "CAPSULE3_PRO", "WINGS2", "SPACE_PRO",
		"AIR5_LLITE", "PearClipPro_Ldac", "S15", "S22",
	} {
		if strings.HasPrefix(fw, code) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------- avancé

// SendRaw émet une trame arbitraire. Réservé au panneau avancé.
// Aucune réémission : une commande inconnue peut avoir un effet de bord.
func (a *App) SendRaw(cmdHex, payloadHex string, eqVendor bool) string {
	c := a.conn()
	if c == nil {
		return "non connecté"
	}
	cb, err := hex.DecodeString(strings.ReplaceAll(cmdHex, " ", ""))
	if err != nil || len(cb) != 2 {
		return "commande invalide : attendu 2 octets hex (ex. 0306)"
	}
	var pb []byte
	if strings.TrimSpace(payloadHex) != "" {
		pb, err = hex.DecodeString(strings.ReplaceAll(payloadHex, " ", ""))
		if err != nil {
			return "payload hex invalide"
		}
	}
	cmd := peats.Command{cb[0], cb[1]}
	f, ok := c.Set(cmd, cmd, pb)
	if !ok {
		a.log("→ %s %s — pas de réponse", cmd, strings.ToUpper(payloadHex))
		return ""
	}
	a.log("→ %s ← %s payload=%s", cmd, f.Command,
		strings.ToUpper(hex.EncodeToString(f.Payload)))
	return ""
}
