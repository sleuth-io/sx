package utils

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExpandTilde(t *testing.T) {
	homeDir, _ := os.UserHomeDir()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:  "tilde only",
			input: "~",
			want:  homeDir,
		},
		{
			name:  "tilde with path",
			input: "~/test",
			want:  filepath.Join(homeDir, "test"),
		},
		{
			name:  "absolute path",
			input: "/absolute/path",
			want:  "/absolute/path",
		},
		{
			name:  "relative path",
			input: "relative/path",
			want:  "relative/path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ExpandTilde(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ExpandTilde() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ExpandTilde() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFileExists(t *testing.T) {
	// Create a temporary file
	tmpfile, err := os.CreateTemp("", "test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())
	tmpfile.Close()

	tests := []struct {
		name string
		path string
		want bool
	}{
		{
			name: "existing file",
			path: tmpfile.Name(),
			want: true,
		},
		{
			name: "non-existing file",
			path: "/non/existing/path/file.txt",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FileExists(tt.path); got != tt.want {
				t.Errorf("FileExists() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEnsureDir(t *testing.T) {
	tmpDir := os.TempDir()
	testDir := filepath.Join(tmpDir, "test-ensure-dir", "nested", "path")
	defer os.RemoveAll(filepath.Join(tmpDir, "test-ensure-dir"))

	if err := EnsureDir(testDir); err != nil {
		t.Errorf("EnsureDir() error = %v", err)
	}

	// Verify directory was created
	if !FileExists(testDir) {
		t.Errorf("EnsureDir() did not create directory")
	}

	// Calling again should not error
	if err := EnsureDir(testDir); err != nil {
		t.Errorf("EnsureDir() on existing dir error = %v", err)
	}
}

func TestResolveCommand(t *testing.T) {
	tests := []struct {
		name        string
		command     string
		installPath string
		want        string
	}{
		{"bare command", "node", "/opt/install", "node"},
		{"bare command uv", "uv", "/opt/install", "uv"},
		{"relative path", "./bin/server", "/opt/install", "/opt/install/bin/server"},
		{"absolute path", "/usr/bin/node", "/opt/install", "/usr/bin/node"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveCommand(tt.command, tt.installPath)
			if got != tt.want {
				t.Errorf("ResolveCommand(%q) = %q, want %q", tt.command, got, tt.want)
			}
		})
	}
}

func TestResolveArgs(t *testing.T) {
	// Create a temp dir with a real file to test os.Stat logic
	installPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(installPath, "server.py"), []byte(""), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(installPath, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installPath, "src", "index.js"), []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	args := []string{"run", "server.py", "-v", "src/index.js", "/abs/path", "nonexistent.txt"}
	got := ResolveArgs(args, installPath)

	// "run" - not a file, stays as-is
	if got[0] != "run" {
		t.Errorf("arg[0] = %q, want \"run\"", got[0])
	}
	// "server.py" - exists, made absolute
	if got[1] != filepath.Join(installPath, "server.py") {
		t.Errorf("arg[1] = %q, want absolute path", got[1])
	}
	// "-v" - flag, stays as-is
	if got[2] != "-v" {
		t.Errorf("arg[2] = %q, want \"-v\"", got[2])
	}
	// "src/index.js" - exists, made absolute
	if got[3] != filepath.Join(installPath, "src/index.js") {
		t.Errorf("arg[3] = %q, want absolute path", got[3])
	}
	// "/abs/path" - already absolute, stays as-is
	if got[4] != "/abs/path" {
		t.Errorf("arg[4] = %q, want \"/abs/path\"", got[4])
	}
	// "nonexistent.txt" - doesn't exist, stays as-is
	if got[5] != "nonexistent.txt" {
		t.Errorf("arg[5] = %q, want \"nonexistent.txt\"", got[5])
	}
}

func TestURLHash(t *testing.T) {
	tests := []struct {
		name string
		url1 string
		url2 string
		same bool
	}{
		{
			name: "same URLs produce same hash",
			url1: "https://example.com/test",
			url2: "https://example.com/test",
			same: true,
		},
		{
			name: "different URLs produce different hash",
			url1: "https://example.com/test1",
			url2: "https://example.com/test2",
			same: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash1 := URLHash(tt.url1)
			hash2 := URLHash(tt.url2)

			if (hash1 == hash2) != tt.same {
				t.Errorf("URLHash() same = %v, want %v", hash1 == hash2, tt.same)
			}

			// Verify hash is not empty
			if hash1 == "" || hash2 == "" {
				t.Error("URLHash() returned empty string")
			}
		})
	}
}
