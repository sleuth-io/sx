package clients

import (
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/metadata"
)

// supportingMockClient is a mockClient that always supports a given asset type.
// (registry_test.go's newMockClient sets supported types to nil, so
// SupportsAssetType returns false for everything — not what we want here.)
type supportingMockClient struct {
	mockClient
	supports map[asset.Type]bool
}

func (m *supportingMockClient) SupportsAssetType(t asset.Type) bool { return m.supports[t] }

func newSupportingMock(id string, types ...asset.Type) *supportingMockClient {
	m := &supportingMockClient{
		mockClient: mockClient{BaseClient: NewBaseClient(id, id, nil)},
		supports:   make(map[asset.Type]bool, len(types)),
	}
	for _, t := range types {
		m.supports[t] = true
	}
	return m
}

func TestOrchestrator_filterAssets_ClientsField(t *testing.T) {
	o := &Orchestrator{}
	scope := &InstallScope{Type: ScopeGlobal}

	mkBundle := func(name string, clients []string) *AssetBundle {
		return &AssetBundle{
			Asset: &lockfile.Asset{Name: name, Type: asset.TypeSkill},
			Metadata: &metadata.Metadata{
				Asset: metadata.Asset{
					Name:    name,
					Version: "1.0.0",
					Type:    asset.TypeSkill,
					Clients: clients,
				},
			},
		}
	}

	gemini := newSupportingMock("gemini", asset.TypeSkill)
	claude := newSupportingMock("claude-code", asset.TypeSkill)

	t.Run("empty Clients installs to all clients", func(t *testing.T) {
		bundles := []*AssetBundle{mkBundle("a", nil)}

		got := o.filterAssets(bundles, gemini, scope)
		if len(got) != 1 {
			t.Errorf("gemini should receive asset with empty Clients: got %d", len(got))
		}
		got = o.filterAssets(bundles, claude, scope)
		if len(got) != 1 {
			t.Errorf("claude-code should receive asset with empty Clients: got %d", len(got))
		}
	})

	t.Run("listed client receives asset", func(t *testing.T) {
		bundles := []*AssetBundle{mkBundle("a", []string{"claude-code"})}

		got := o.filterAssets(bundles, claude, scope)
		if len(got) != 1 {
			t.Errorf("claude-code is in Clients list: got %d", len(got))
		}
	})

	t.Run("non-listed client does not receive asset", func(t *testing.T) {
		bundles := []*AssetBundle{mkBundle("a", []string{"claude-code"})}

		got := o.filterAssets(bundles, gemini, scope)
		if len(got) != 0 {
			t.Errorf("gemini is not in Clients list, but received the asset")
		}
	})

	t.Run("multi-client list filters correctly", func(t *testing.T) {
		bundles := []*AssetBundle{mkBundle("a", []string{"claude-code", "cursor"})}

		got := o.filterAssets(bundles, claude, scope)
		if len(got) != 1 {
			t.Errorf("claude-code is listed: got %d", len(got))
		}
		got = o.filterAssets(bundles, gemini, scope)
		if len(got) != 0 {
			t.Errorf("gemini is not listed: got %d", len(got))
		}
	})

	t.Run("client filter does not override type incompatibility", func(t *testing.T) {
		// Even if a client is listed in [asset].clients, the asset type
		// must still be supported by the client. The client filter is a
		// further restriction on top of capability support, not a bypass.
		hookOnly := newSupportingMock("claude-code", asset.TypeHook) // does not support Skill
		bundles := []*AssetBundle{mkBundle("a", []string{"claude-code"})}

		got := o.filterAssets(bundles, hookOnly, scope)
		if len(got) != 0 {
			t.Errorf("client doesn't support skill asset type, filter shouldn't bypass: got %d", len(got))
		}
	})
}
