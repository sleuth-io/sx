package utils

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
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

// TestCreateZipPreservesExecutableBit guards against SK-465: the executable
// bit on hook script files used to be stripped at publish time, leaving
// installed hooks unable to run.
func TestCreateZipPreservesExecutableBit(t *testing.T) {
	srcDir := t.TempDir()
	scriptPath := filepath.Join(srcDir, "script.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	// Some filesystems honor umask on WriteFile; force the mode explicitly.
	if err := os.Chmod(scriptPath, 0755); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	zipData, err := CreateZip(srcDir)
	if err != nil {
		t.Fatalf("CreateZip: %v", err)
	}

	r, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	var found bool
	for _, f := range r.File {
		if f.Name != "script.sh" {
			continue
		}
		found = true
		if f.Mode()&0111 == 0 {
			t.Errorf("expected executable bit on zip entry, got mode %o", f.Mode())
		}
	}
	if !found {
		t.Fatal("script.sh entry not found in zip")
	}
}

// TestAddFileToZipPreservesExecutableBit guards against SK-465: when sx add
// rewrites metadata.toml in a packaged asset, copyZipFile used to drop file
// modes for every other entry, stripping +x from hook scripts.
func TestAddFileToZipPreservesExecutableBit(t *testing.T) {
	srcDir := t.TempDir()
	scriptPath := filepath.Join(srcDir, "log-session.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	if err := os.Chmod(scriptPath, 0755); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	zipData, err := CreateZip(srcDir)
	if err != nil {
		t.Fatalf("CreateZip: %v", err)
	}

	updated, err := AddFileToZip(zipData, "metadata.toml", []byte("[asset]\nname = \"log-stop\"\n"))
	if err != nil {
		t.Fatalf("AddFileToZip: %v", err)
	}

	r, err := zip.NewReader(bytes.NewReader(updated), int64(len(updated)))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	var found bool
	for _, f := range r.File {
		if f.Name != "log-session.sh" {
			continue
		}
		found = true
		if f.Mode()&0111 == 0 {
			t.Errorf("expected executable bit preserved through AddFileToZip, got mode %o", f.Mode())
		}
	}
	if !found {
		t.Fatal("log-session.sh entry not found in zip after AddFileToZip")
	}

	// And it must round-trip back to disk with +x via ExtractZip.
	outDir := t.TempDir()
	if err := ExtractZip(updated, outDir); err != nil {
		t.Fatalf("ExtractZip: %v", err)
	}
	info, err := os.Stat(filepath.Join(outDir, "log-session.sh"))
	if err != nil {
		t.Fatalf("stat extracted script: %v", err)
	}
	if info.Mode()&0111 == 0 {
		t.Errorf("expected extracted script to be executable, got mode %o", info.Mode())
	}
}
