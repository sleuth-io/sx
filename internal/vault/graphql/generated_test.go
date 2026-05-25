package graphql

import (
	"bytes"
	"os"
	"testing"

	"github.com/Khan/genqlient/generate"
)

// TestGeneratedUpToDate runs genqlient against the local schema and
// operations/*.graphql and compares the result to the committed
// generated.go. If they differ, someone edited a .graphql file or the
// schema without running `make gql-generate`.
//
// Why this lives as a Go test rather than only in `make gql-check`:
// CI runs `go test ./...` but does not run `make prepush`, so without
// this test a stale generated.go could merge unnoticed.
func TestGeneratedUpToDate(t *testing.T) {
	cfg, err := generate.ReadAndValidateConfig("genqlient.yaml")
	if err != nil {
		t.Fatalf("read genqlient.yaml: %v", err)
	}

	generated, err := generate.Generate(cfg)
	if err != nil {
		t.Fatalf("genqlient codegen failed: %v", err)
	}

	want, ok := generated["generated.go"]
	if !ok {
		t.Fatal("genqlient did not produce generated.go")
	}

	got, err := os.ReadFile("generated.go")
	if err != nil {
		t.Fatalf("read generated.go: %v", err)
	}

	if !bytes.Equal(want, got) {
		t.Fatal("generated.go is out of date. Run `make gql-generate` and commit the result.")
	}
}
