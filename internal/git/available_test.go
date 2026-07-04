package git

import (
	"context"
	"testing"
)

func TestCheckAvailabilityFindsGit(t *testing.T) {
	// The dev/CI environment always has a working git.
	av := CheckAvailability(context.Background())
	if !av.Available {
		t.Fatalf("git should be available here, got reason %q", av.Reason)
	}
	if av.Version == "" || av.Reason != "" {
		t.Errorf("availability = %+v, want version set and no reason", av)
	}
}

func TestCheckAvailabilityMissingGit(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	av := CheckAvailability(context.Background())
	if av.Available {
		t.Fatal("git should not be found with an empty PATH")
	}
	if av.Reason == "" {
		t.Error("a missing git should carry a user-facing reason")
	}
}
