package clients

import (
	"slices"
	"sort"
	"testing"

	"github.com/sleuth-io/sx/internal/metadata"
)

// TestClientIDsMatchMetadataValidation guards against drift between the
// authoritative ClientID* constants in this package and the validation set
// used by metadata.Asset.Clients. They live in two places to avoid a
// metadata→clients import cycle, but they must agree: a client added here
// without updating metadata's set is silently rejected by `sx add` even
// though installation would work, and vice versa.
func TestClientIDsMatchMetadataValidation(t *testing.T) {
	got := metadata.SortedValidClientIDs()
	want := append([]string(nil), AllClientIDs()...)
	sort.Strings(want)

	if !slices.Equal(got, want) {
		t.Errorf("client ID drift: metadata accepts %v, clients package registers %v", got, want)
	}
}
