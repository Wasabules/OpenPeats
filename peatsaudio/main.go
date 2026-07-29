package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"peatsaudio/internal/i18n"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// Pick the system language up before the window exists, so anything Go
	// renders on its own — a notification fired early — is already localised.
	// The frontend overrides this through App.SetLanguage as soon as it knows
	// the user's actual choice.
	i18n.SetLocale(i18n.DetectLocale())

	app := NewApp()

	err := wails.Run(&options.App{
		Title:     "PeatsAudio",
		Width:     1180,
		Height:    860,
		MinWidth:  900,
		MinHeight: 640,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 14, G: 16, B: 22, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind: []interface{}{
			app,
		},
	})
	if err != nil {
		log.Fatalf("wails: %v", err)
	}
}
