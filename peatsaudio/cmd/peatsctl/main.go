// peatsctl — command line tool to check the RFCOMM transport.
//
// Mostly used to validate a platform whose transport has never run: it
// exercises connection, reads and, on request, a write, without depending on
// the graphical interface.
//
//	peatsctl -addr 98:80:BB:40:D4:ED
//	peatsctl -addr ... -anc 1
//	peatsctl -addr ... -lang fr
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"peatsaudio/internal/i18n"
	"peatsaudio/internal/peats"
)

func main() {
	// The locale has to be settled before the flags are declared: usage
	// strings are rendered at declaration time, so waiting for flag.Parse
	// would print -h in the system language even when -lang asked otherwise.
	i18n.SetLocale(i18n.DetectLocale())
	if tag, ok := localeFromArgs(os.Args[1:]); ok {
		i18n.SetLocale(tag)
	}

	lang := flag.String("lang", i18n.Locale(), i18n.T("cli.flag.lang"))
	addr := flag.String("addr", "", i18n.T("cli.flag.addr"))
	channel := flag.Int("channel", 0, i18n.T("cli.flag.channel"))
	anc := flag.Int("anc", -1, i18n.T("cli.flag.anc"))
	flag.Parse()

	i18n.SetLocale(*lang)

	if strings.TrimSpace(*addr) == "" {
		fmt.Fprintln(os.Stderr, i18n.T("cli.addrRequired"))
		fmt.Fprintln(os.Stderr, i18n.T("cli.addrHint"))
		flag.Usage()
		os.Exit(2)
	}

	fmt.Println(i18n.T("cli.connecting", "addr", *addr))
	c, err := peats.Connect(*addr, uint8(*channel))
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.T("cli.connectFailed"), explain(err))
		os.Exit(1)
	}
	defer c.Close()
	fmt.Printf("%s\n\n", i18n.T("cli.connected", "channel", c.Channel()))

	reads := []struct {
		labelKey string
		cmd      peats.Command
		show     func(peats.Frame) string
	}{
		{"read.batteryLeft", peats.CmdBatteryLeft, pct},
		{"read.batteryRight", peats.CmdBatteryRight, pct},
		{"read.batteryCase", peats.CmdBatteryCase, pct},
		{"read.firmware", peats.CmdFirmware, firmware},
		{"read.ancMode", peats.CmdANCModeGet, ancMode},
		{"read.ancLevel", peats.CmdANCLevelGet, activeLevel},
		{"read.ldac", peats.CmdLDACGet, onOff},
		{"read.multipoint", peats.CmdMultipointGet, onOff},
		{"read.gameMode", peats.CmdGameModeGet, onOff},
	}

	ok := 0
	for _, r := range reads {
		label := i18n.T(r.labelKey)
		f, err := c.Get(r.cmd)
		if err != nil {
			fmt.Printf("  %-20s %s  — %s\n", label, r.cmd, i18n.T("cli.noResponse"))
			continue
		}
		ok++
		fmt.Printf("  %-20s %s  %-22X %s\n", label, r.cmd, f.Payload, r.show(f))
	}
	fmt.Printf("\n%s\n", i18n.T("cli.readsSucceeded", "ok", ok, "total", len(reads)))

	if *anc >= 0 {
		fmt.Printf("\n%s\n", i18n.T("cli.writingANC", "mode", *anc))
		// Never retried: several `set` commands take effect without
		// acknowledging, and resending one replays the side effect.
		c.Set(peats.CmdANCModeSet, peats.CmdANCModeGet, []byte{byte(*anc)})
		if f, err := c.Get(peats.CmdANCModeGet); err == nil {
			fmt.Println(i18n.T("cli.readBack", "value", ancMode(f)))
		} else {
			fmt.Fprintln(os.Stderr, i18n.T("cli.readBackFailed"), err)
		}
	}

	if ok == 0 {
		fmt.Fprintln(os.Stderr, "\n"+i18n.T("cli.transportDead"))
		os.Exit(1)
	}
}

// localeFromArgs picks -lang out of the raw arguments, in every spelling the
// flag package accepts, so the locale is known before any flag is declared.
func localeFromArgs(args []string) (string, bool) {
	for i, a := range args {
		switch {
		case a == "-lang", a == "--lang":
			if i+1 < len(args) {
				return args[i+1], true
			}
		case strings.HasPrefix(a, "-lang="):
			return strings.TrimPrefix(a, "-lang="), true
		case strings.HasPrefix(a, "--lang="):
			return strings.TrimPrefix(a, "--lang="), true
		}
	}
	return "", false
}

func explain(err error) string {
	switch {
	case errors.Is(err, peats.ErrBusy):
		return i18n.T("err.busy")
	case errors.Is(err, peats.ErrUnreachable):
		return i18n.T("err.unreachable")
	case errors.Is(err, peats.ErrNoChannel):
		return i18n.T("err.noChannel")
	case errors.Is(err, peats.ErrUnsupported):
		return i18n.T("err.unsupported")
	default:
		return err.Error()
	}
}

func pct(f peats.Frame) string {
	return i18n.T("value.percent", "value", f.Scalar())
}

func firmware(f peats.Frame) string {
	if len(f.Payload) > 1 {
		return strings.TrimRight(string(f.Payload[1:]), "\x00")
	}
	return i18n.T("value.unknown")
}

func onOff(f peats.Frame) string {
	if f.Scalar() == 1 {
		return i18n.T("value.on")
	}
	return i18n.T("value.off")
}

func ancMode(f peats.Frame) string {
	switch f.Scalar() {
	case peats.ANCOff:
		return i18n.T("anc.off")
	case peats.ANCOn:
		return i18n.T("anc.on")
	case peats.ANCTransparency:
		return i18n.T("anc.transparency")
	case peats.ANCAdaptive:
		return i18n.T("anc.adaptive")
	default:
		return fmt.Sprintf("0x%02X", f.Scalar())
	}
}

func activeLevel(f peats.Frame) string {
	code, ok := f.ActiveCode()
	if !ok {
		return i18n.T("value.noneActive")
	}
	keys := map[byte]string{
		peats.ANCLevelAdaptive: "ancLevel.adaptive",
		peats.ANCLevelIndoor:   "ancLevel.indoor",
		peats.ANCLevelOutdoor:  "ancLevel.outdoor",
		peats.ANCLevelTraffic:  "ancLevel.traffic",
	}
	if key, hit := keys[code]; hit {
		return i18n.T(key)
	}
	return fmt.Sprintf("0x%02X", code)
}
