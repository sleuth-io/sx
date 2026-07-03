package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"

	_ "github.com/sleuth-io/sx/internal/clients/claude_code"    // Register Claude Code client
	_ "github.com/sleuth-io/sx/internal/clients/cline"          // Register Cline client
	_ "github.com/sleuth-io/sx/internal/clients/codex"          // Register Codex client
	_ "github.com/sleuth-io/sx/internal/clients/cursor"         // Register Cursor client
	_ "github.com/sleuth-io/sx/internal/clients/gemini"         // Register Gemini Code Assist client
	_ "github.com/sleuth-io/sx/internal/clients/github_copilot" // Register GitHub Copilot client
	_ "github.com/sleuth-io/sx/internal/clients/kiro"           // Register Kiro client
	_ "github.com/sleuth-io/sx/internal/clients/openclaw"       // Register OpenClaw client
	_ "github.com/sleuth-io/sx/internal/clients/opencode"       // Register OpenCode client
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:     "sx",
		Width:     1200,
		Height:    800,
		MinWidth:  880,
		MinHeight: 560,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 250, G: 250, B: 249, A: 1},
		OnStartup:        app.startup,
		DragAndDrop: &options.DragAndDrop{
			EnableFileDrop:     true,
			DisableWebViewDrop: true,
		},
		Bind: []any{
			app,
		},
		Mac: &mac.Options{
			TitleBar:             mac.TitleBarHiddenInset(),
			About:                &mac.AboutInfo{Title: "sx", Message: "Your team's library for AI assets"},
			WebviewIsTransparent: false,
		},
	})
	if err != nil {
		println("Error:", err.Error())
	}
}
