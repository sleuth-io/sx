package vault

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/sleuth-io/sx/internal/vault/layout"
)

func storageZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatalf("create zip entry %s: %v", name, err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatalf("write zip entry %s: %v", name, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

func mustLayout(t *testing.T, v layout.Version) layout.Layout {
	t.Helper()
	l, err := layout.ForVersion(v)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func readFileString(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func mustNotExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("%s should not exist (err=%v)", path, err)
	}
}

func TestStoreAssetVersionV1(t *testing.T) {
	root := t.TempDir()
	l := mustLayout(t, layout.V1)

	zipData := storageZip(t, map[string]string{
		"SKILL.md":      "# v1 skill",
		"metadata.toml": "[asset]\nname = \"chat\"\n",
	})
	if err := storeAssetVersion(root, l, "chat", "1.0", zipData); err != nil {
		t.Fatal(err)
	}

	if got := readFileString(t, filepath.Join(root, "assets", "chat", "1.0", "SKILL.md")); got != "# v1 skill" {
		t.Errorf("SKILL.md = %q", got)
	}
	if got := readFileString(t, filepath.Join(root, "assets", "chat", "list.txt")); got != "1.0\n" {
		t.Errorf("list.txt = %q", got)
	}
	// v1 must not materialize a root view or create an archive
	mustNotExist(t, filepath.Join(root, "assets", "chat", "SKILL.md"))
	mustNotExist(t, filepath.Join(root, ".sx", "versions"))
}

func TestStoreAssetVersionV2(t *testing.T) {
	root := t.TempDir()
	l := mustLayout(t, layout.V2)

	if err := storeAssetVersion(root, l, "chat", "1.0", storageZip(t, map[string]string{
		"SKILL.md":            "# one",
		"metadata.toml":       "[asset]\nname = \"chat\"\n",
		"references/notes.md": "ref one",
	})); err != nil {
		t.Fatal(err)
	}

	// Archive holds the immutable copy
	if got := readFileString(t, filepath.Join(root, ".sx", "versions", "chat", "1.0", "SKILL.md")); got != "# one" {
		t.Errorf("archive SKILL.md = %q", got)
	}
	if got := readFileString(t, filepath.Join(root, ".sx", "versions", "chat", "list.txt")); got != "1.0\n" {
		t.Errorf("list.txt = %q", got)
	}
	// Root view mirrors the latest version, including subdirectories
	if got := readFileString(t, filepath.Join(root, "assets", "chat", "SKILL.md")); got != "# one" {
		t.Errorf("root view SKILL.md = %q", got)
	}
	if got := readFileString(t, filepath.Join(root, "assets", "chat", "references", "notes.md")); got != "ref one" {
		t.Errorf("root view references/notes.md = %q", got)
	}
	// No version directories or list.txt pollute the root view
	mustNotExist(t, filepath.Join(root, "assets", "chat", "1.0"))
	mustNotExist(t, filepath.Join(root, "assets", "chat", "list.txt"))

	// Publishing a second version refreshes the root view
	if err := storeAssetVersion(root, l, "chat", "2.0", storageZip(t, map[string]string{
		"SKILL.md":      "# two",
		"metadata.toml": "[asset]\nname = \"chat\"\n",
	})); err != nil {
		t.Fatal(err)
	}
	if got := readFileString(t, filepath.Join(root, "assets", "chat", "SKILL.md")); got != "# two" {
		t.Errorf("root view after publish = %q", got)
	}
	// Files dropped in 2.0 must not linger in the root view from 1.0
	mustNotExist(t, filepath.Join(root, "assets", "chat", "references", "notes.md"))
	// The 1.0 archive is untouched
	if got := readFileString(t, filepath.Join(root, ".sx", "versions", "chat", "1.0", "SKILL.md")); got != "# one" {
		t.Errorf("1.0 archive mutated: %q", got)
	}
}

func TestStoreAssetVersionV2OlderVersionKeepsLatestView(t *testing.T) {
	root := t.TempDir()
	l := mustLayout(t, layout.V2)

	for _, v := range []string{"2.0", "1.0"} {
		if err := storeAssetVersion(root, l, "chat", v, storageZip(t, map[string]string{
			"SKILL.md":      "# " + v,
			"metadata.toml": "[asset]\nname = \"chat\"\n",
		})); err != nil {
			t.Fatal(err)
		}
	}
	// Backfilling an older version must not demote the root view
	if got := readFileString(t, filepath.Join(root, "assets", "chat", "SKILL.md")); got != "# 2.0" {
		t.Errorf("root view = %q, want latest (2.0)", got)
	}
}

func TestDeleteAssetStorageV2(t *testing.T) {
	root := t.TempDir()
	l := mustLayout(t, layout.V2)
	for _, v := range []string{"1.0", "2.0"} {
		if err := storeAssetVersion(root, l, "chat", v, storageZip(t, map[string]string{
			"SKILL.md":      "# " + v,
			"metadata.toml": "[asset]\nname = \"chat\"\n",
		})); err != nil {
			t.Fatal(err)
		}
	}

	// Deleting the latest version re-materializes the previous one
	if err := deleteAssetStorage(root, l, "chat", "2.0"); err != nil {
		t.Fatal(err)
	}
	if got := readFileString(t, filepath.Join(root, "assets", "chat", "SKILL.md")); got != "# 1.0" {
		t.Errorf("root view after delete = %q, want 1.0", got)
	}
	mustNotExist(t, filepath.Join(root, ".sx", "versions", "chat", "2.0"))

	// Deleting the last version removes the asset entirely
	if err := deleteAssetStorage(root, l, "chat", "1.0"); err != nil {
		t.Fatal(err)
	}
	mustNotExist(t, filepath.Join(root, "assets", "chat"))
	mustNotExist(t, filepath.Join(root, ".sx", "versions", "chat"))
}

func TestDeleteAssetStorageV2WholeAsset(t *testing.T) {
	root := t.TempDir()
	l := mustLayout(t, layout.V2)
	if err := storeAssetVersion(root, l, "chat", "1.0", storageZip(t, map[string]string{
		"SKILL.md":      "# one",
		"metadata.toml": "[asset]\nname = \"chat\"\n",
	})); err != nil {
		t.Fatal(err)
	}
	if err := deleteAssetStorage(root, l, "chat", ""); err != nil {
		t.Fatal(err)
	}
	mustNotExist(t, filepath.Join(root, "assets", "chat"))
	mustNotExist(t, filepath.Join(root, ".sx", "versions", "chat"))
}

func TestRenameAssetStorageV2(t *testing.T) {
	root := t.TempDir()
	l := mustLayout(t, layout.V2)
	if err := storeAssetVersion(root, l, "chat", "1.0", storageZip(t, map[string]string{
		"SKILL.md":      "# one",
		"metadata.toml": "name = \"chat\"\n\n[asset]\nname = \"chat\"\n",
	})); err != nil {
		t.Fatal(err)
	}

	if err := renameAssetStorage(root, l, "chat", "converse"); err != nil {
		t.Fatal(err)
	}
	if got := readFileString(t, filepath.Join(root, "assets", "converse", "SKILL.md")); got != "# one" {
		t.Errorf("renamed root view = %q", got)
	}
	if got := readFileString(t, filepath.Join(root, ".sx", "versions", "converse", "1.0", "SKILL.md")); got != "# one" {
		t.Errorf("renamed archive = %q", got)
	}
	mustNotExist(t, filepath.Join(root, "assets", "chat"))
	mustNotExist(t, filepath.Join(root, ".sx", "versions", "chat"))
}

func TestRenameAssetStorageV2TargetExists(t *testing.T) {
	root := t.TempDir()
	l := mustLayout(t, layout.V2)
	for _, name := range []string{"chat", "converse"} {
		if err := storeAssetVersion(root, l, name, "1.0", storageZip(t, map[string]string{
			"SKILL.md":      "# x",
			"metadata.toml": "[asset]\nname = \"x\"\n",
		})); err != nil {
			t.Fatal(err)
		}
	}
	if err := renameAssetStorage(root, l, "chat", "converse"); err == nil {
		t.Error("rename onto existing asset should fail")
	}
}

func TestSourcePathsEqual(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"./assets/chat/1.0", "assets/chat/1.0", true},
		{"assets/chat/1.0", "assets/chat/1.0", true},
		{"assets/chat/1.0/", "assets/chat/1.0", true},
		{"assets/chat/2.0", "assets/chat/1.0", false},
		{".sx/versions/chat/1.0", "assets/chat/1.0", false},
	}
	for _, c := range cases {
		if got := sourcePathsEqual(c.a, c.b); got != c.want {
			t.Errorf("sourcePathsEqual(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
