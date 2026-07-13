package main

import (
	"os"
	"regexp"
	"testing"
)

// The permission universe lives in two places by design: the loader's
// KNOWN_PERMISSIONS (host.ts, gates enable) and knownPluginPermissions
// (plugins.go, gates publish). They must accept the same set — a
// permission added to only one side either publishes extensions that
// then refuse to load, or (as with "benchmarks" in 2.2.3) silently
// blocks the marketplace's Install button while the CLI happily
// publishes the same bundle.
func TestKnownPluginPermissionsMirrorsLoader(t *testing.T) {
	src, err := os.ReadFile("frontend/src/plugins/host.ts")
	if err != nil {
		t.Fatalf("read host.ts: %v", err)
	}
	block := regexp.MustCompile(`(?s)KNOWN_PERMISSIONS = new Set\(\[(.*?)\]\)`).FindSubmatch(src)
	if block == nil {
		t.Fatalf("KNOWN_PERMISSIONS set not found in host.ts — update this test's parser")
	}
	loader := map[string]bool{}
	for _, m := range regexp.MustCompile(`"([^"]+)"`).FindAllSubmatch(block[1], -1) {
		loader[string(m[1])] = true
	}
	if len(loader) == 0 {
		t.Fatalf("parsed zero permissions from host.ts")
	}

	for p := range loader {
		if !knownPluginPermissions[p] {
			t.Errorf("host.ts KNOWN_PERMISSIONS has %q but plugins.go knownPluginPermissions does not — publish would reject what the loader accepts", p)
		}
	}
	for p := range knownPluginPermissions {
		if !loader[p] {
			t.Errorf("plugins.go knownPluginPermissions has %q but host.ts KNOWN_PERMISSIONS does not — publish would accept what the loader rejects", p)
		}
	}
}
