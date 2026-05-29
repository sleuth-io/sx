package sxvault

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetAssetZipReadsManifestSourcePathAssets(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "sx.toml"), `schema_version = 1
created_by = "test"

[[assets]]
  name = "hetchy-delivery"
  version = "0.1.0"
  type = "skill"
  clients = ["claude-code"]
  [assets.source-path]
    path = "sx-assets/skills/hetchy-delivery"

  [[assets.scopes]]
    kind = "org"
`)
	assetDir := filepath.Join(root, "sx-assets", "skills", "hetchy-delivery")
	writeFile(t, filepath.Join(assetDir, "metadata.toml"), `metadata-version = "1.0"

[asset]
name = "hetchy-delivery"
version = "0.1.0"
type = "skill"
description = "Shared Hetchy delivery workflow."
clients = ["claude-code"]

[skill]
prompt-file = "SKILL.md"
`)
	writeFile(t, filepath.Join(assetDir, "SKILL.md"), "# Hetchy Delivery\n")

	client, err := OpenPath(root, PathOptions{})
	if err != nil {
		t.Fatalf("OpenPath: %v", err)
	}
	got, err := client.GetAssetZip(t.Context(), "hetchy-delivery", "")
	if err != nil {
		t.Fatalf("GetAssetZip: %v", err)
	}
	if got.Name != "hetchy-delivery" || got.Version != "0.1.0" || got.Type != "skill" {
		t.Fatalf("asset zip summary = %+v", got)
	}
	assertZipContains(t, got.Data, "SKILL.md", "# Hetchy Delivery")
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
		t.Fatal(err)
	}
}

func assertZipContains(t *testing.T, data []byte, name, want string) {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}
	for _, file := range zr.File {
		if file.Name != name {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			t.Fatalf("open %s: %v", name, err)
		}
		defer rc.Close()
		var buf bytes.Buffer
		if _, err := buf.ReadFrom(rc); err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if !strings.Contains(buf.String(), want) {
			t.Fatalf("%s = %q, want to contain %q", name, buf.String(), want)
		}
		return
	}
	t.Fatalf("zip missing %s", name)
}
