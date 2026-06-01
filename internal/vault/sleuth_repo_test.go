package vault

import "testing"

func TestTrailingOwnerName(t *testing.T) {
	cases := map[string]string{
		"https://github.com/acme/repo":      "acme/repo",
		"https://github.com/acme/repo.git":  "acme/repo",
		"github.com/acme/Repo":              "acme/repo",
		"acme/repo":                         "acme/repo",
		"git@github.com:acme/repo.git":      "acme/repo",
		"  https://gitlab.com/g/acme/repo ": "acme/repo",
		"repo":                              "repo",
	}
	for in, want := range cases {
		if got := trailingOwnerName(in); got != want {
			t.Errorf("trailingOwnerName(%q) = %q, want %q", in, got, want)
		}
	}
}
