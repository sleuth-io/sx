package main

import "testing"

func TestBaseVersionNewer(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"1.8.1", "1.8.1", false},
		{"1.8.1", "1.8.0", true},
		{"1.8.1", "1.8.2", false},
		// A git-describe build ahead of the tag is NOT outdated.
		{"1.8.1", "1.8.1-31-gee44d6d", false},
		{"1.8.2", "1.8.1-31-gee44d6d", true},
		{"2.0.0", "1.9.9", true},
	}
	for _, c := range cases {
		if got := baseVersionNewer(c.latest, c.current); got != c.want {
			t.Errorf("baseVersionNewer(%q, %q) = %v, want %v", c.latest, c.current, got, c.want)
		}
	}
}

func TestIsDevBuild(t *testing.T) {
	for v, want := range map[string]bool{
		"dev":                     true,
		"":                        true,
		"1.8.1-31-gee44d6d":       true,
		"1.8.1-31-gee44d6d-dirty": true,
		"1.8.1":                   false,
	} {
		if got := isDevBuild(v); got != want {
			t.Errorf("isDevBuild(%q) = %v, want %v", v, got, want)
		}
	}
}
