package layout

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/sleuth-io/sx/internal/manifest"
)

func TestForVersion(t *testing.T) {
	for _, v := range []Version{V1, V2} {
		l, err := ForVersion(v)
		if err != nil {
			t.Fatalf("ForVersion(%d): %v", v, err)
		}
		if l.Version() != v {
			t.Errorf("Version() = %d, want %d", l.Version(), v)
		}
	}
	if _, err := ForVersion(3); err == nil {
		t.Error("ForVersion(3) should fail")
	}
	if _, err := ForVersion(0); err == nil {
		t.Error("ForVersion(0) should fail")
	}
}

func TestV1Paths(t *testing.T) {
	l, _ := ForVersion(V1)
	cases := []struct{ got, want string }{
		{l.AssetDir("chat"), filepath.Join("assets", "chat")},
		{l.VersionsDir("chat"), filepath.Join("assets", "chat")},
		{l.VersionDir("chat", "1.0"), filepath.Join("assets", "chat", "1.0")},
		{l.VersionListPath("chat"), filepath.Join("assets", "chat", "list.txt")},
		{l.MetadataPath("chat", "1.0"), filepath.Join("assets", "chat", "1.0", "metadata.toml")},
		{l.SourcePathRel("chat", "1.0"), "assets/chat/1.0"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
}

func TestV2Paths(t *testing.T) {
	l, _ := ForVersion(V2)
	cases := []struct{ got, want string }{
		{l.AssetDir("chat"), filepath.Join("assets", "chat")},
		{l.VersionsDir("chat"), filepath.Join(".sx", "versions", "chat")},
		{l.VersionDir("chat", "1.0"), filepath.Join(".sx", "versions", "chat", "1.0")},
		{l.VersionListPath("chat"), filepath.Join(".sx", "versions", "chat", "list.txt")},
		{l.MetadataPath("chat", "1.0"), filepath.Join(".sx", "versions", "chat", "1.0", "metadata.toml")},
		{l.SourcePathRel("chat", "1.0"), ".sx/versions/chat/1.0"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
}

func writeManifest(t *testing.T, root string, schemaVersion int) {
	t.Helper()
	m := &manifest.Manifest{SchemaVersion: schemaVersion}
	if err := manifest.Save(root, m); err != nil {
		t.Fatalf("save manifest: %v", err)
	}
}

func TestDetectFromManifest(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, 1)
	l, err := Detect(root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if l.Version() != V1 {
		t.Errorf("Version() = %d, want V1", l.Version())
	}
}

func TestDetectFromManifestV2(t *testing.T) {
	if manifest.CurrentSchemaVersion < 2 {
		t.Skip("manifest schema 2 not yet current; Parse would reject it")
	}
	root := t.TempDir()
	writeManifest(t, root, 2)
	l, err := Detect(root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if l.Version() != V2 {
		t.Errorf("Version() = %d, want V2", l.Version())
	}
}

func TestDetectFutureSchemaFails(t *testing.T) {
	root := t.TempDir()
	// Write a raw manifest with a schema version beyond what this build
	// understands; Detect must propagate ErrUnsupportedSchema loudly.
	data := []byte("schema_version = 99\n")
	if err := os.WriteFile(filepath.Join(root, manifest.FileName), data, 0644); err != nil {
		t.Fatal(err)
	}
	_, err := Detect(root)
	if !errors.Is(err, manifest.ErrUnsupportedSchema) {
		t.Errorf("Detect error = %v, want ErrUnsupportedSchema", err)
	}
}

func TestDetectByDirectoryShape(t *testing.T) {
	// No manifest, .sx/versions present → v2.
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".sx", "versions", "chat"), 0755); err != nil {
		t.Fatal(err)
	}
	l, err := Detect(root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if l.Version() != V2 {
		t.Errorf("Version() = %d, want V2", l.Version())
	}

	// No manifest, only assets/ present → v1 (legacy).
	root = t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "assets", "chat", "1.0"), 0755); err != nil {
		t.Fatal(err)
	}
	l, err = Detect(root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if l.Version() != V1 {
		t.Errorf("Version() = %d, want V1", l.Version())
	}
}

func TestDetectEmptyRootUsesCurrentDefault(t *testing.T) {
	root := t.TempDir()
	l, err := Detect(root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if l.Version() != Version(manifest.CurrentSchemaVersion) {
		t.Errorf("Version() = %d, want current default %d", l.Version(), manifest.CurrentSchemaVersion)
	}
}
