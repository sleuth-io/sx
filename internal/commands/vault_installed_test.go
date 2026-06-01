package commands

import (
	"sort"
	"testing"

	"github.com/sleuth-io/sx/internal/assets"
)

func namesOfInstalled(in []assets.InstalledAsset) []string {
	out := make([]string, 0, len(in))
	for _, a := range in {
		out = append(out, a.Name)
	}
	sort.Strings(out)
	return out
}

func eqStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// `vault list --installed --profile X` shows assets installed by X. An empty
// profile counts as the current profile, so untagged leftovers show under
// whatever profile you're using. Assets stamped with a different profile are
// hidden.
func TestFilterInstalledForProfile(t *testing.T) {
	all := []assets.InstalledAsset{
		{Name: "gh-skill", Type: "skill", Profile: "gh"},
		{Name: "default-skill", Type: "skill", Profile: "default"},
		{Name: "untagged-skill", Type: "skill", Profile: ""}, // empty = current profile
		{Name: "gh-agent", Type: "agent", Profile: "gh"},
	}

	t.Run("current profile gh, no type filter", func(t *testing.T) {
		got := namesOfInstalled(filterInstalledForProfile(all, "gh", ""))
		want := []string{"gh-agent", "gh-skill", "untagged-skill"}
		if !eqStrings(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("current profile default", func(t *testing.T) {
		got := namesOfInstalled(filterInstalledForProfile(all, "default", ""))
		want := []string{"default-skill", "untagged-skill"}
		if !eqStrings(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("type filter applies on top of profile", func(t *testing.T) {
		got := namesOfInstalled(filterInstalledForProfile(all, "gh", "skill"))
		want := []string{"gh-skill", "untagged-skill"}
		if !eqStrings(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
}
