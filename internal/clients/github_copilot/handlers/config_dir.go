package handlers

import (
	"path/filepath"

	"github.com/sleuth-io/sx/v2/internal/utils"
)

// GlobalConfigDir resolves the GLOBAL GitHub Copilot configuration root — the
// directory that holds skills, instructions, prompts, agents and the CLI
// mcp-config.json.
//
// COPILOT_HOME, when set, *is* that root: it replaces ~/.copilot wholesale
// rather than naming a parent under which a .copilot directory gets created.
// So COPILOT_HOME=/opt/profile resolves to /opt/profile, never
// /opt/profile/.copilot. When it is unset or blank the root is the default
// ~/.copilot. This mirrors the GitHub docs, which state that COPILOT_HOME
// changes the ~/.copilot/ segment itself:
// https://docs.github.com/en/copilot/how-tos/copilot-cli/set-up-copilot-cli/configure-copilot-cli#editing-trusted-directories
//
// A leading ~ is expanded and a relative value is made absolute, so the
// returned path is always absolute. This governs global scope only; repo and
// path scopes stay rooted at a repo root and never consult COPILOT_HOME.
func GlobalConfigDir() (string, error) {
	base, set, err := utils.ResolveHomeEnv("COPILOT_HOME")
	if err != nil {
		return "", err
	}
	if set {
		return base, nil
	}
	return filepath.Join(base, ConfigDir), nil
}
