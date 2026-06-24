package vault

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sleuth-io/sx/internal/lockfile"
)

// TestSleuthVault_AddAsset_PermissionDeniedCarriesGID verifies that a 403 from
// the upload endpoint surfaces as an *AssetEditPermissionError carrying the
// asset GID the server handed back — the signal `sx add` uses to offer a PR.
func TestSleuthVault_AddAsset_PermissionDeniedCarriesGID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/skills/assets" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success":           false,
			"error":             "You do not have permission to edit this asset.",
			"permission_denied": true,
			"skill_id":          "QXNzZXQ6MTIz",
			"skill_slug":        "foo_skill",
		})
	}))
	t.Cleanup(srv.Close)

	v := NewSleuthVault(srv.URL, "test-token")
	_, err := v.AddAssetWithResult(context.Background(), &lockfile.Asset{Name: "foo", Version: "1"}, []byte("zip"))

	var denial *AssetEditPermissionError
	if !errors.As(err, &denial) {
		t.Fatalf("AddAssetWithResult err = %v, want *AssetEditPermissionError", err)
	}
	if denial.AssetGID != "QXNzZXQ6MTIz" {
		t.Fatalf("AssetGID = %q, want QXNzZXQ6MTIz", denial.AssetGID)
	}
	if denial.Asset != "foo_skill" {
		t.Fatalf("Asset = %q, want foo_skill (server slug)", denial.Asset)
	}
}

// TestSleuthVault_OpenAssetPullRequest_CreatesPRAndAddsFiles verifies the PR
// flow: create the PR for the asset GID, then add one file change per asset
// file — splitting nested paths and skipping metadata.toml — and return the
// server's source URL.
func TestSleuthVault_OpenAssetPullRequest_CreatesPRAndAddsFiles(t *testing.T) {
	srv, records := mockSleuthGraphQL(t, map[string]func(map[string]any) any{
		"CreateAssetPullRequest": func(map[string]any) any {
			return map[string]any{
				"createAssetPullRequest": map[string]any{
					"pullRequest": map[string]any{
						"id":        "UFI6OTk=",
						"sourceUrl": "https://skills.example/pr/99",
					},
					"errors": []any{},
				},
			}
		},
		"AddAssetPullRequestFileChange": func(map[string]any) any {
			return map[string]any{
				"addAssetPullRequestFileChange": map[string]any{
					"success": true,
					"errors":  []any{},
				},
			}
		},
	})

	zipData := makeZip(t, map[string]string{
		"metadata.toml":  "name = \"foo\"\n",
		"SKILL.md":       "# Foo\n",
		"scripts/run.sh": "echo hi\n",
	})

	v := NewSleuthVault(srv.URL, "test-token")
	res, err := v.OpenAssetPullRequest(context.Background(), "QXNzZXQ6MTIz", "Add foo 2", "body", zipData)
	if err != nil {
		t.Fatalf("OpenAssetPullRequest err = %v", err)
	}
	if !res.Created || res.URL != "https://skills.example/pr/99" {
		t.Fatalf("PRResult = %+v, want Created with the source URL", res)
	}

	// One create + one file change per non-metadata file.
	var created int
	fileChanges := map[string]any{} // name -> path (nil for root)
	var assetID string
	for _, rec := range *records {
		input, _ := rec.Variables["input"].(map[string]any)
		switch rec.OperationName {
		case "CreateAssetPullRequest":
			created++
			assetID, _ = input["assetId"].(string)
		case "AddAssetPullRequestFileChange":
			name, _ := input["name"].(string)
			fileChanges[name] = input["path"]
		}
	}

	if created != 1 {
		t.Fatalf("CreateAssetPullRequest called %d times, want 1", created)
	}
	if assetID != "QXNzZXQ6MTIz" {
		t.Fatalf("createAssetPullRequest assetId = %q, want QXNzZXQ6MTIz", assetID)
	}
	if _, ok := fileChanges["metadata.toml"]; ok {
		t.Fatalf("metadata.toml must not be sent as a file change; got changes %v", fileChanges)
	}
	if len(fileChanges) != 2 {
		t.Fatalf("file changes = %v, want exactly SKILL.md and scripts/run.sh", fileChanges)
	}
	if p, ok := fileChanges["SKILL.md"]; !ok || p != nil {
		t.Fatalf("SKILL.md path = %v (present=%v), want nil (root file)", p, ok)
	}
	if p := fileChanges["run.sh"]; p != "scripts" {
		t.Fatalf("run.sh path = %v, want \"scripts\"", p)
	}
}

func makeZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create %q: %v", name, err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("zip write %q: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}
