package codex

import (
	"strings"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/clients"
	"github.com/sleuth-io/sx/internal/clients/codex/handlers"
)

// RuleCapabilities exposes Codex-owned single-file detection. Codex does
// not support sx-managed rules, so only asset type detection is populated.
func RuleCapabilities() *clients.RuleCapabilities {
	return &clients.RuleCapabilities{
		ClientName:      clients.ClientIDCodex,
		DetectAssetType: detectAssetType,
	}
}

// RuleCapabilities returns Codex's client-owned single-file detection hooks.
func (c *Client) RuleCapabilities() *clients.RuleCapabilities {
	return RuleCapabilities()
}

func detectAssetType(path string, _ []byte) *asset.Type {
	lower := strings.ToLower(path)
	agentDir := handlers.ConfigDir + "/" + handlers.DirAgents + "/"

	if strings.Contains(lower, agentDir) && strings.HasSuffix(lower, ".toml") {
		return &asset.TypeAgent
	}
	return nil
}
