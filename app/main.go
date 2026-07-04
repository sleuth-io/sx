package main

import (
	"embed"
	"log"
	goruntime "runtime"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
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

	// Native menu. macOS gets the standard app menu (with Settings… living
	// there per platform convention) and an Edit menu so clipboard
	// shortcuts work in the webview; Cmd+, / Ctrl+, opens Settings
	// everywhere.
	appMenu := menu.NewMenu()
	if goruntime.GOOS == "darwin" {
		sxMenu := appMenu.AddSubmenu("sx")
		sxMenu.AddSeparator()
		sxMenu.AddText("Settings…", keys.CmdOrCtrl(","), func(*menu.CallbackData) {
			app.OpenSettings()
		})
		sxMenu.AddSeparator()
		sxMenu.AddText("Quit sx", keys.CmdOrCtrl("q"), func(*menu.CallbackData) {
			app.Quit()
		})
		appMenu.Append(menu.EditMenu())
	} else {
		fileMenu := appMenu.AddSubmenu("File")
		fileMenu.AddText("Settings…", keys.CmdOrCtrl(","), func(*menu.CallbackData) {
			app.OpenSettings()
		})
		fileMenu.AddSeparator()
		fileMenu.AddText("Quit", keys.CmdOrCtrl("q"), func(*menu.CallbackData) {
			app.Quit()
		})
	}

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
		OnShutdown:       app.shutdown,
		Menu:             appMenu,
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
		log.Fatal(err)
	}
}
