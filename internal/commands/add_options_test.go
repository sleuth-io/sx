package commands

import (
	"testing"

	"github.com/sleuth-io/sx/internal/lockfile"
)

func TestGetScopes(t *testing.T) {
	tests := []struct {
		name           string
		opts           addOptions
		expectInherit  bool
		expectRemove   bool
		expectScopes   []lockfile.Scope
		expectEntity   string
		expectScopeNil bool
	}{
		{
			name:          "yes without scope flags returns Inherit",
			opts:          addOptions{Yes: true},
			expectInherit: true,
		},
		{
			name:         "yes with scope-global returns global scopes",
			opts:         addOptions{Yes: true, ScopeGlobal: true},
			expectScopes: []lockfile.Scope{},
		},
		{
			name: "yes with scope-repo returns repo scopes",
			opts: addOptions{Yes: true, ScopeRepos: []string{"https://github.com/org/repo"}},
			expectScopes: []lockfile.Scope{
				{Repo: "https://github.com/org/repo"},
			},
		},
		{
			name: "yes with scope-repo and paths",
			opts: addOptions{Yes: true, ScopeRepos: []string{"https://github.com/org/repo#backend,frontend"}},
			expectScopes: []lockfile.Scope{
				{Repo: "https://github.com/org/repo", Paths: []string{"backend", "frontend"}},
			},
		},
		{
			name:         "yes with scope entity returns entity",
			opts:         addOptions{Yes: true, Scope: "personal"},
			expectEntity: "personal",
		},
		{
			name:         "no-install without scope flags returns Remove",
			opts:         addOptions{NoInstall: true},
			expectRemove: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.opts.getScopes()
			if err != nil {
				t.Fatalf("getScopes() error = %v", err)
			}

			if result.Inherit != tt.expectInherit {
				t.Errorf("Inherit = %v, want %v", result.Inherit, tt.expectInherit)
			}
			if result.Remove != tt.expectRemove {
				t.Errorf("Remove = %v, want %v", result.Remove, tt.expectRemove)
			}
			if tt.expectEntity != "" && result.ScopeEntity != tt.expectEntity {
				t.Errorf("ScopeEntity = %q, want %q", result.ScopeEntity, tt.expectEntity)
			}
			if tt.expectScopes != nil {
				if len(result.Scopes) != len(tt.expectScopes) {
					t.Fatalf("Scopes length = %d, want %d", len(result.Scopes), len(tt.expectScopes))
				}
				for i, s := range tt.expectScopes {
					if result.Scopes[i].Repo != s.Repo {
						t.Errorf("Scopes[%d].Repo = %q, want %q", i, result.Scopes[i].Repo, s.Repo)
					}
					if len(result.Scopes[i].Paths) != len(s.Paths) {
						t.Errorf("Scopes[%d].Paths length = %d, want %d", i, len(result.Scopes[i].Paths), len(s.Paths))
					}
				}
			}
		})
	}
}
