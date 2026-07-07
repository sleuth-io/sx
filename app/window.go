package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/sleuth-io/sx/internal/utils"
)

// The window remembers its size and position across launches — saved on
// shutdown, restored on startup. Best-effort: a vanished display or a
// corrupt state file just falls back to the default geometry.

type windowState struct {
	Width  int `json:"width"`
	Height int `json:"height"`
	X      int `json:"x"`
	Y      int `json:"y"`
}

func windowStatePath() (string, error) {
	dir, err := utils.GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "app-window.json"), nil
}

// restoreWindowState applies the previous session's geometry, if any.
func (a *App) restoreWindowState(ctx context.Context) {
	path, err := windowStatePath()
	if err != nil {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var state windowState
	if err := json.Unmarshal(data, &state); err != nil {
		return
	}
	if state.Width < 400 || state.Height < 300 {
		return
	}
	wailsruntime.WindowSetSize(ctx, state.Width, state.Height)
	wailsruntime.WindowSetPosition(ctx, state.X, state.Y)
}

// saveWindowState records the current geometry for the next launch.
func (a *App) saveWindowState(ctx context.Context) {
	path, err := windowStatePath()
	if err != nil {
		return
	}
	width, height := wailsruntime.WindowGetSize(ctx)
	x, y := wailsruntime.WindowGetPosition(ctx)
	state := windowState{Width: width, Height: height, X: x, Y: y}
	data, err := json.Marshal(state)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0644)
}
