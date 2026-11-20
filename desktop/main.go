package main

import (
	"embed"
	"fmt"
	"log"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"openpeats/internal/i18n"
	"openpeats/internal/launch"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// Pick the system language up before the window exists, so anything Go
	// renders on its own — a notification fired early — is already localised.
	// The frontend overrides this through App.SetLanguage as soon as it knows
	// the user's actual choice.
	i18n.SetLocale(i18n.DetectLocale())

	opts, err := launch.Parse(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.T("args.invalidANC"))
		os.Exit(2)
	}

	// Installing the desktop integration needs no window and no instance.
	if opts.Integration != "" {
		os.Exit(integrationCommand(opts.Integration))
	}

	app := NewApp()

	// A second launch hands its request to the running instance and stops
	// here: the earbuds take one control link, and two instances would
	// fight over it.
	shell, forwarded := newDesktop(app, opts)
	if forwarded {
		return
	}
	app.desktop = shell

	err = wails.Run(&options.App{
		Title:     "OpenPeats",
		Width:     1180,
		Height:    860,
		MinWidth:  900,
		MinHeight: 640,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 14, G: 16, B: 22, A: 1},
		// Without the window only when something in the panel shows the
		// application runs: never invisible with no way back.
		StartHidden:   !opts.Window() && shell.hasIndicator(),
		OnStartup:     app.startup,
		OnBeforeClose: app.beforeClose,
		OnShutdown:    app.shutdown,
		Bind: []interface{}{
			app,
		},
	})
	if err != nil {
		log.Fatalf("wails: %v", err)
	}
}
