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
	"github.com/sleuth-io/sx/internal/config"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := NewApp()

	// Native menu. macOS gets the standard app menu (with Settings… living
	// there per platform convention), an Edit menu so clipboard shortcuts
	// work in the webview, and the standard Window menu; other platforms
	// fold Settings/Quit into File. File's New items and Help are the same
	// everywhere — menu clicks become frontend events, since the frontend
	// owns the create flows.
	emit := func(event string) func(*menu.CallbackData) {
		return func(*menu.CallbackData) { app.emitMenuEvent(event) }
	}
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
	}

	fileMenu := appMenu.AddSubmenu("File")
	fileMenu.AddText("New Skill…", keys.CmdOrCtrl("n"), emit("new-skill"))
	fileMenu.AddText("New Collection…", keys.Combo("n", keys.CmdOrCtrlKey, keys.ShiftKey), emit("new-collection"))
	fileMenu.AddText("New Library…", nil, emit("new-library"))
	if goruntime.GOOS != "darwin" {
		fileMenu.AddSeparator()
		fileMenu.AddText("Settings…", keys.CmdOrCtrl(","), func(*menu.CallbackData) {
			app.OpenSettings()
		})
		fileMenu.AddSeparator()
		fileMenu.AddText("Quit", keys.CmdOrCtrl("q"), func(*menu.CallbackData) {
			app.Quit()
		})
	}

	if goruntime.GOOS == "darwin" {
		appMenu.Append(menu.EditMenu())
		appMenu.Append(menu.WindowMenu())
	}

	helpMenu := appMenu.AddSubmenu("Help")
	helpMenu.AddText("sx Documentation", nil, func(*menu.CallbackData) {
		_ = config.OpenBrowser("https://github.com/sleuth-io/sx#readme")
	})

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
