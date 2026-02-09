package utils

import (
	"archive/zip"
	"bytes"
	"testing"
)

func createTestZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	w := zip.NewWriter(buf)
	for name, content := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatalf("Failed to create zip entry %q: %v", name, err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatalf("Failed to write zip entry %q: %v", name, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Failed to close zip: %v", err)
	}
	return buf.Bytes()
}

func TestHasContentFiles_MetadataOnly(t *testing.T) {
	zipData := createTestZip(t, map[string]string{
		"metadata.toml": "[asset]\nname = \"test\"\n",
	})
	has, err := HasContentFiles(zipData)
	if err != nil {
		t.Fatalf("HasContentFiles failed: %v", err)
	}
	if has {
		t.Error("Expected false for metadata-only zip")
	}
}

func TestHasContentFiles_WithContentFiles(t *testing.T) {
	zipData := createTestZip(t, map[string]string{
		"metadata.toml": "[asset]\nname = \"test\"\n",
		"server.js":     "console.log('hello')",
		"package.json":  "{}",
	})
	has, err := HasContentFiles(zipData)
	if err != nil {
		t.Fatalf("HasContentFiles failed: %v", err)
	}
	if !has {
		t.Error("Expected true for zip with content files")
	}
}

func TestHasContentFiles_WithDirectoriesOnly(t *testing.T) {
	// Zip with metadata.toml and an empty directory should return false
	buf := new(bytes.Buffer)
	w := zip.NewWriter(buf)

	f, _ := w.Create("metadata.toml")
	f.Write([]byte("[asset]"))

	// Add a directory entry
	header := &zip.FileHeader{Name: "subdir/"}
	header.SetMode(0755)
	w.CreateHeader(header)
	w.Close()

	has, err := HasContentFiles(buf.Bytes())
	if err != nil {
		t.Fatalf("HasContentFiles failed: %v", err)
	}
	if has {
		t.Error("Expected false for zip with only directories and metadata.toml")
	}
}

func TestHasContentFiles_InvalidZip(t *testing.T) {
	_, err := HasContentFiles([]byte("not a zip"))
	if err == nil {
		t.Error("Expected error for invalid zip data")
	}
}
