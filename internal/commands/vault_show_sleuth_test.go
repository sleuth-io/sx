package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// mockSleuthVaultServer stands in for the skills.new GraphQL API for end-to-end
// `sx vault show` tests. It answers the two operations the command issues:
//
//   - VaultAssetLookup  — slug/name resolution (GetAssetDetails)
//   - AssetInstallations — current install scopes (resolveCurrentTargets)
//
// The asset "personal-skill-2" has display name "Personal skill 2" and is
// installed org-wide, so the rendered output is deterministic. Any non-/graphql
// path (e.g. the metadata.toml fetch) 404s, which the command tolerates.
func mockSleuthVaultServer(t *testing.T) *httptest.Server {
	t.Helper()
	skillNode := map[string]any{
		"__typename":  "Skill",
		"slug":        "personal-skill-2",
		"name":        "Personal skill 2",
		"type":        "SKILL",
		"description": "Use for personal stuff",
		"createdAt":   "2026-06-09T13:26:58Z",
		"updatedAt":   "2026-06-09T13:26:58Z",
		"versions": []any{
			map[string]any{"version": "1", "createdAt": "2026-06-09T13:26:58Z", "filesCount": 1},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/graphql" {
			http.NotFound(w, r) // metadata.toml etc. — command ignores the failure
			return
		}
		body, _ := io.ReadAll(r.Body)
		var req struct {
			OperationName string         `json:"operationName"`
			Variables     map[string]any `json:"variables"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		var data any
		switch req.OperationName {
		case "VaultAssetLookup":
			// Exact slug, or the display-name fallback search, both find the asset.
			var nodes []any
			if req.Variables["slug"] == "personal-skill-2" || req.Variables["search"] == "Personal skill 2" {
				nodes = []any{skillNode}
			}
			data = map[string]any{"vault": map[string]any{"assets": map[string]any{"nodes": nodes}}}
		case "AssetInstallations":
			data = map[string]any{"vault": map[string]any{"assets": map[string]any{
				"pageInfo": map[string]any{"hasNextPage": false, "endCursor": nil},
				"nodes": []any{map[string]any{
					"__typename": "Skill",
					"slug":       "personal-skill-2",
					"name":       "Personal skill 2",
					"installations": []any{map[string]any{
						"entityType": "ORGANIZATION", "entityName": "org",
						"entityRef": nil, "entityId": "gid-org", "monoRepoConfigId": nil,
					}},
				}},
			}}}
		default:
			http.Error(w, "unexpected operation: "+req.OperationName, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestVaultShowSleuth_ResolvesBySlugAndName drives the whole `sx vault show`
// command (cobra → createVault → GetAssetDetails → scope resolution → output)
// against a mock skills.new vault, proving the asset resolves whether the user
// passes the canonical slug or the display name. This guards the end-to-end
// behavior so a future change to the command can't silently break either path.
func TestVaultShowSleuth_ResolvesBySlugAndName(t *testing.T) {
	cases := []struct {
		name string
		arg  string
	}{
		{"by slug", "personal-skill-2"},
		{"by display name", "Personal skill 2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := NewTestEnv(t)
			srv := mockSleuthVaultServer(t)
			configDir := env.MkdirAll(filepath.Join(env.HomeDir, ".config", "sx"))
			env.WriteFile(
				filepath.Join(configDir, "config.json"),
				fmt.Sprintf(`{"type":"sleuth","serverUrl":%q,"authToken":"test-token"}`, srv.URL),
			)

			cmd := NewVaultCommand()
			cmd.SetArgs([]string{"show", tc.arg})
			var stdout bytes.Buffer
			cmd.SetOut(&stdout)

			if err := cmd.Execute(); err != nil {
				t.Fatalf("vault show %q failed: %v", tc.arg, err)
			}

			out := stdout.String()
			// Display name is always shown, regardless of which form was queried.
			if !strings.Contains(out, "Personal skill 2") {
				t.Errorf("expected display name in output, got:\n%s", out)
			}
			// The org-wide install renders as the global scope line.
			if !strings.Contains(out, "Global") {
				t.Errorf("expected global scope in output, got:\n%s", out)
			}
		})
	}
}

// TestVaultShowSleuth_NotFound proves an unknown identifier surfaces a not-found
// error rather than rendering an empty asset.
func TestVaultShowSleuth_NotFound(t *testing.T) {
	env := NewTestEnv(t)
	srv := mockSleuthVaultServer(t)
	configDir := env.MkdirAll(filepath.Join(env.HomeDir, ".config", "sx"))
	env.WriteFile(
		filepath.Join(configDir, "config.json"),
		fmt.Sprintf(`{"type":"sleuth","serverUrl":%q,"authToken":"test-token"}`, srv.URL),
	)

	cmd := NewVaultCommand()
	cmd.SetArgs([]string{"show", "does-not-exist"})
	cmd.SetOut(&bytes.Buffer{})

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not-found error, got: %v", err)
	}
}
