package vault

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/mgmt"
)

// mockSleuthGraphQL spins up a test server that dispatches on the
// operationName in the genqlient-style request body. handlers maps
// operationName -> handler that receives the parsed variables map and
// returns the JSON object to use as the "data" field. The handler can
// also record the raw request for assertions via the returned recorder.
type sleuthGQLRecord struct {
	OperationName string
	Variables     map[string]any
	RawBody       string
}

func mockSleuthGraphQL(t *testing.T, handlers map[string]func(vars map[string]any) any) (*httptest.Server, *[]sleuthGQLRecord) {
	t.Helper()
	var records []sleuthGQLRecord
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/graphql" {
			http.NotFound(w, r)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var req struct {
			OperationName string         `json:"operationName"`
			Variables     map[string]any `json:"variables"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		records = append(records, sleuthGQLRecord{
			OperationName: req.OperationName,
			Variables:     req.Variables,
			RawBody:       string(body),
		})
		h, ok := handlers[req.OperationName]
		if !ok {
			http.Error(w, "unexpected operation: "+req.OperationName, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": h(req.Variables)})
	}))
	t.Cleanup(srv.Close)
	return srv, &records
}

// TestSleuthVault_ListTeams_QueryShape locks in the PR141 bug fix: the
// ListTeams query must nest under organization { teams { ... } }, not the
// long-gone root teams field. We assert sx parses the nested-organization
// shape correctly and projects it to mgmt.Team.
func TestSleuthVault_ListTeams_QueryShape(t *testing.T) {
	srv, records := mockSleuthGraphQL(t, map[string]func(map[string]any) any{
		"ListTeams": func(vars map[string]any) any {
			return map[string]any{
				"organization": map[string]any{
					"teams": map[string]any{
						"nodes": []any{
							map[string]any{
								"id":   "team-1",
								"name": "platform",
								"adminMembers": []any{
									map[string]any{"id": "u1", "email": "a@example.com"},
								},
								"members": map[string]any{
									"totalCount": 2,
									"nodes": []any{
										map[string]any{"id": "u1", "email": "a@example.com"},
										map[string]any{"id": "u2", "email": "b@example.com"},
									},
								},
								"skillsRepositories": []any{
									map[string]any{"repositoryId": "repo-9", "owner": "org", "name": "repo-9"},
								},
							},
						},
						"totalCount": 1,
						"pageInfo": map[string]any{
							"hasNextPage": false,
							"endCursor":   nil,
						},
					},
				},
			}
		},
	})

	v := NewSleuthVault(srv.URL, "test-token")
	result, err := v.ListTeams(context.Background(), ListTeamsOptions{Limit: 100})
	if err != nil {
		t.Fatalf("ListTeams failed: %v", err)
	}
	if len(result.Teams) != 1 || result.Teams[0].Name != "platform" {
		t.Fatalf("unexpected teams: %+v", result.Teams)
	}
	if len(*records) != 1 || (*records)[0].OperationName != "ListTeams" {
		t.Fatalf("expected single ListTeams request, got: %+v", *records)
	}
	// $first variable must be sent so the server caps the page.
	if _, ok := (*records)[0].Variables["first"]; !ok {
		t.Errorf("expected $first variable in ListTeams request, got: %+v", (*records)[0].Variables)
	}
}

func TestSleuthVault_RemoveBotTeamSendsEmptyTeamIDs(t *testing.T) {
	srv, records := mockSleuthGraphQL(t, map[string]func(map[string]any) any{
		"ListBots": func(vars map[string]any) any {
			return map[string]any{
				"bots": []any{
					map[string]any{
						"id":              "bot-1",
						"name":            "testers",
						"slug":            "testers",
						"description":     "Tests stuff",
						"teams":           []any{map[string]any{"id": "team-1", "name": "Dev"}},
						"installedSkills": []any{},
					},
				},
			}
		},
		"UpdateBot": func(vars map[string]any) any {
			input, ok := vars["input"].(map[string]any)
			if !ok {
				t.Fatalf("UpdateBot input = %T, want object", vars["input"])
			}
			teamIDs, ok := input["teamIds"].([]any)
			if !ok {
				t.Fatalf("UpdateBot teamIds = %#v (%T), want empty list", input["teamIds"], input["teamIds"])
			}
			if len(teamIDs) != 0 {
				t.Fatalf("UpdateBot teamIds = %#v, want empty list", teamIDs)
			}
			return map[string]any{
				"updateBot": map[string]any{
					"bot":    map[string]any{"id": "bot-1", "name": "testers"},
					"errors": []any{},
				},
			}
		},
	})

	v := NewSleuthVault(srv.URL, "test-token")
	if err := v.RemoveBotTeam(context.Background(), "testers", "Dev"); err != nil {
		t.Fatalf("RemoveBotTeam failed: %v", err)
	}

	var ops []string
	for _, rec := range *records {
		ops = append(ops, rec.OperationName)
	}
	if got := strings.Join(ops, ","); got != "ListBots,ListBots,UpdateBot" {
		t.Fatalf("operations = %s", got)
	}
}

// TestSleuthVault_FindUser_QueryShape locks in the PR142 bug fix: the
// FindUser query nests under organization { users(term:) }. Tested via
// userGIDByEmail, the only call site.
func TestSleuthVault_FindUser_QueryShape(t *testing.T) {
	srv, records := mockSleuthGraphQL(t, map[string]func(map[string]any) any{
		"FindUser": func(vars map[string]any) any {
			term, _ := vars["term"].(string)
			if term == "" {
				t.Errorf("FindUser called without term variable")
			}
			return map[string]any{
				"organization": map[string]any{
					"users": map[string]any{
						"nodes": []any{
							map[string]any{"id": "user-42", "email": "match@example.com"},
							map[string]any{"id": "user-99", "email": "other@example.com"},
						},
					},
				},
			}
		},
	})

	v := NewSleuthVault(srv.URL, "test-token")
	gid, err := v.userGIDByEmail(context.Background(), "match@example.com")
	if err != nil {
		t.Fatalf("userGIDByEmail failed: %v", err)
	}
	if gid != "user-42" {
		t.Errorf("expected gid user-42, got %q", gid)
	}
	if len(*records) != 1 || (*records)[0].OperationName != "FindUser" {
		t.Fatalf("expected single FindUser request, got: %+v", *records)
	}
}

func TestSleuthVault_CreateBotRuntimeToken_QueryShape(t *testing.T) {
	expiresAt := "2026-05-27T12:00:00Z"
	srv, records := mockSleuthGraphQL(t, map[string]func(map[string]any) any{
		"ListBots": func(vars map[string]any) any {
			return map[string]any{
				"bots": []any{
					map[string]any{
						"id":          "bot-1",
						"name":        "Reviewer",
						"slug":        "reviewer",
						"description": "Reviews pull requests.",
						"teams":       []any{},
						"installedSkills": []any{
							map[string]any{"name": "fix-pr", "assetType": "SKILL", "isDirectInstall": true},
							map[string]any{"name": "webapp-testing", "assetType": "SKILL", "isDirectInstall": false},
						},
					},
				},
			}
		},
		"CreateBotRuntimeToken": func(vars map[string]any) any {
			if got, _ := vars["botId"].(string); got != "bot-1" {
				t.Errorf("botId = %q, want bot-1", got)
			}
			if got, _ := vars["label"].(string); got != "Hetchy runtime" {
				t.Errorf("label = %q, want Hetchy runtime", got)
			}
			gotTTL, _ := vars["ttlSeconds"].(float64)
			if int(gotTTL) != 600 {
				t.Errorf("ttlSeconds = %v, want 600", gotTTL)
			}
			return map[string]any{
				"createBotRuntimeToken": map[string]any{
					"botKey":    "runtime-token",
					"expiresAt": expiresAt,
				},
			}
		},
	})

	v := NewSleuthVault(srv.URL, "test-token")
	token, gotExpiresAt, err := v.CreateBotRuntimeToken(context.Background(), "Reviewer", " Hetchy runtime ", 600)
	if err != nil {
		t.Fatalf("CreateBotRuntimeToken failed: %v", err)
	}
	if token != "runtime-token" {
		t.Fatalf("token = %q, want runtime-token", token)
	}
	wantExpiresAt, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil {
		t.Fatal(err)
	}
	if !gotExpiresAt.Equal(wantExpiresAt) {
		t.Fatalf("expiresAt = %s, want %s", gotExpiresAt, wantExpiresAt)
	}
	if len(*records) != 2 || (*records)[0].OperationName != "ListBots" || (*records)[1].OperationName != "CreateBotRuntimeToken" {
		t.Fatalf("unexpected GraphQL operations: %+v", *records)
	}
}

func TestSleuthVault_RevokeBotRuntimeTokens_QueryShape(t *testing.T) {
	srv, records := mockSleuthGraphQL(t, map[string]func(map[string]any) any{
		"ListBots": func(vars map[string]any) any {
			return map[string]any{
				"bots": []any{
					map[string]any{
						"id":          "bot-1",
						"name":        "Reviewer",
						"slug":        "reviewer",
						"description": "Reviews pull requests.",
						"teams":       []any{},
						"installedSkills": []any{
							map[string]any{"name": "fix-pr", "assetType": "SKILL", "isDirectInstall": true},
							map[string]any{"name": "webapp-testing", "assetType": "SKILL", "isDirectInstall": false},
						},
					},
				},
			}
		},
		"RevokeBotRuntimeTokens": func(vars map[string]any) any {
			if got, _ := vars["botId"].(string); got != "bot-1" {
				t.Errorf("botId = %q, want bot-1", got)
			}
			return map[string]any{
				"revokeBotRuntimeTokens": map[string]any{
					"revokedCount": 2,
				},
			}
		},
	})

	v := NewSleuthVault(srv.URL, "test-token")
	revoked, err := v.RevokeBotRuntimeTokens(context.Background(), "Reviewer")
	if err != nil {
		t.Fatalf("RevokeBotRuntimeTokens failed: %v", err)
	}
	if revoked != 2 {
		t.Fatalf("revoked = %d, want 2", revoked)
	}
	if len(*records) != 2 || (*records)[0].OperationName != "ListBots" || (*records)[1].OperationName != "RevokeBotRuntimeTokens" {
		t.Fatalf("unexpected GraphQL operations: %+v", *records)
	}
}

func TestSleuthVault_ListBots_ProjectsSlug(t *testing.T) {
	// Server returns installedSkills in non-alphabetical order with a
	// duplicate plus an installed agent asset to verify the Sleuth path
	// filters to skills, dedupes, and sorts (matching the file-based path's
	// contract on mgmt.Bot.InstalledSkills).
	srv, _ := mockSleuthGraphQL(t, map[string]func(map[string]any) any{
		"ListBots": func(vars map[string]any) any {
			return map[string]any{
				"bots": []any{
					map[string]any{
						"id":          "bot-1",
						"name":        "Reviewer",
						"slug":        "reviewer",
						"description": "Reviews pull requests.",
						"teams":       []any{},
						"installedSkills": []any{
							map[string]any{"name": "webapp-testing", "assetType": "SKILL", "isDirectInstall": false},
							map[string]any{"name": "reviewer-agent", "assetType": "AGENT", "isDirectInstall": true},
							map[string]any{"name": "fix-pr", "assetType": "SKILL", "isDirectInstall": true},
							map[string]any{"name": "fix-pr", "assetType": "SKILL", "isDirectInstall": true},
						},
					},
				},
			}
		},
	})

	v := NewSleuthVault(srv.URL, "test-token")
	bots, err := v.ListBots(context.Background())
	if err != nil {
		t.Fatalf("ListBots failed: %v", err)
	}
	if len(bots) != 1 {
		t.Fatalf("ListBots returned %d bots, want 1", len(bots))
	}
	if bots[0].Slug != "reviewer" {
		t.Fatalf("bots[0].Slug = %q, want reviewer", bots[0].Slug)
	}
	want := []mgmt.BotSkill{
		{Name: "fix-pr", IsDirectInstall: true},
		{Name: "webapp-testing", IsDirectInstall: false},
	}
	if !slices.Equal(bots[0].InstalledSkills, want) {
		t.Fatalf("bots[0].InstalledSkills = %v, want %v (sorted, deduped)", bots[0].InstalledSkills, want)
	}
}

func TestSleuthVault_InstallSkillToBotResolvesListedSkillSlug(t *testing.T) {
	srv, records := mockSleuthGraphQL(t, map[string]func(map[string]any) any{
		"ListBots": func(vars map[string]any) any {
			return map[string]any{
				"bots": []any{
					map[string]any{
						"id":          "bot-1",
						"name":        "testers",
						"slug":        "testers",
						"description": "Tests stuff",
						"teams":       []any{},
					},
				},
			}
		},
		"AssetGID": func(vars map[string]any) any {
			if got := vars["search"]; got != "fix-pr" {
				t.Fatalf("AssetGID search = %v, want fix-pr", got)
			}
			return map[string]any{
				"vault": map[string]any{
					"assets": map[string]any{
						"nodes": []any{
							map[string]any{
								"__typename": "Skill",
								"id":         "skill-1",
								"name":       "Fix PR",
								"slug":       "fix-pr",
							},
						},
					},
				},
			}
		},
		"InstallSkillToBot": func(vars map[string]any) any {
			if got := vars["botId"]; got != "bot-1" {
				t.Fatalf("InstallSkillToBot botId = %v, want bot-1", got)
			}
			if got := vars["skillId"]; got != "skill-1" {
				t.Fatalf("InstallSkillToBot skillId = %v, want skill-1", got)
			}
			return map[string]any{
				"installSkillToBot": map[string]any{
					"success": true,
					"errors":  []any{},
				},
			}
		},
	})

	v := NewSleuthVault(srv.URL, "test-token")
	if err := v.SetAssetInstallation(context.Background(), "fix-pr", InstallTarget{Kind: InstallKindBot, Bot: "testers"}); err != nil {
		t.Fatalf("SetAssetInstallation by slug failed: %v", err)
	}

	var ops []string
	for _, rec := range *records {
		ops = append(ops, rec.OperationName)
	}
	if got := strings.Join(ops, ","); got != "ListBots,AssetGID,InstallSkillToBot" {
		t.Fatalf("operations = %s", got)
	}
}

// TestSleuthVault_InstallSkillToBot_SlugMatchOrderIndependent covers the
// case where a slug-matching asset is preceded by unrelated (non-matching)
// assets in the search response. The slug match must still win — the
// resolver must not depend on the asset's position in the response.
func TestSleuthVault_InstallSkillToBot_SlugMatchOrderIndependent(t *testing.T) {
	srv, _ := mockSleuthGraphQL(t, map[string]func(map[string]any) any{
		"ListBots": func(vars map[string]any) any {
			return map[string]any{
				"bots": []any{
					map[string]any{
						"id":          "bot-1",
						"name":        "testers",
						"slug":        "testers",
						"description": "Tests stuff",
						"teams":       []any{},
					},
				},
			}
		},
		"AssetGID": func(vars map[string]any) any {
			return map[string]any{
				"vault": map[string]any{
					"assets": map[string]any{
						"nodes": []any{
							// Search-prefix matches that don't equal the
							// input listed first so that a first-match
							// policy would return the wrong id.
							map[string]any{
								"__typename": "Skill",
								"id":         "wrong-id",
								"name":       "fix-pr-extras",
								"slug":       "fix-pr-extras",
							},
							map[string]any{
								"__typename": "Skill",
								"id":         "right-id",
								"name":       "Fix PR",
								"slug":       "fix-pr",
							},
						},
					},
				},
			}
		},
		"InstallSkillToBot": func(vars map[string]any) any {
			if got := vars["skillId"]; got != "right-id" {
				t.Fatalf("InstallSkillToBot skillId = %v, want right-id (slug match)", got)
			}
			return map[string]any{
				"installSkillToBot": map[string]any{
					"success": true,
					"errors":  []any{},
				},
			}
		},
	})

	v := NewSleuthVault(srv.URL, "test-token")
	if err := v.SetAssetInstallation(context.Background(), "fix-pr", InstallTarget{Kind: InstallKindBot, Bot: "testers"}); err != nil {
		t.Fatalf("SetAssetInstallation: %v", err)
	}
}

func TestSleuthVault_InstallSkillToBotPrefersExactSlug(t *testing.T) {
	srv, _ := mockSleuthGraphQL(t, map[string]func(map[string]any) any{
		"ListBots": func(vars map[string]any) any {
			return map[string]any{
				"bots": []any{
					map[string]any{
						"id":          "bot-1",
						"name":        "testers",
						"slug":        "testers",
						"description": "Tests stuff",
						"teams":       []any{},
					},
				},
			}
		},
		"AssetGID": func(vars map[string]any) any {
			return map[string]any{
				"vault": map[string]any{
					"assets": map[string]any{
						"nodes": []any{
							map[string]any{
								"__typename": "Skill",
								"id":         "slug-asset",
								"name":       "Architecture Blueprint",
								"slug":       "architecture-blueprint-generator",
							},
							map[string]any{
								"__typename": "Skill",
								"id":         "uploaded-skill",
								"name":       "architecture-blueprint-generator",
								"slug":       "architecture-blueprint-generator_skill",
							},
						},
					},
				},
			}
		},
		"InstallSkillToBot": func(vars map[string]any) any {
			if got := vars["skillId"]; got != "slug-asset" {
				t.Fatalf("InstallSkillToBot skillId = %v, want slug-asset", got)
			}
			return map[string]any{
				"installSkillToBot": map[string]any{
					"success": true,
					"errors":  []any{},
				},
			}
		},
	})

	v := NewSleuthVault(srv.URL, "test-token")
	if err := v.SetAssetInstallation(context.Background(), "architecture-blueprint-generator", InstallTarget{Kind: InstallKindBot, Bot: "testers"}); err != nil {
		t.Fatalf("SetAssetInstallation: %v", err)
	}
}

// TestSleuthVault_InstallSkillToBot_PrefersSlugOverName covers the case
// where the asset-search response contains both a slug-matching asset and
// a *different* display-name-matching asset. The exact slug wins because
// slugs are unique and are what SX exposes from list and upload calls.
func TestSleuthVault_InstallSkillToBot_PrefersSlugOverName(t *testing.T) {
	srv, _ := mockSleuthGraphQL(t, map[string]func(map[string]any) any{
		"ListBots": func(vars map[string]any) any {
			return map[string]any{
				"bots": []any{
					map[string]any{
						"id":          "bot-1",
						"name":        "testers",
						"slug":        "testers",
						"description": "Tests stuff",
						"teams":       []any{},
					},
				},
			}
		},
		"AssetGID": func(vars map[string]any) any {
			return map[string]any{
				"vault": map[string]any{
					"assets": map[string]any{
						"nodes": []any{
							map[string]any{
								"__typename": "Skill",
								"id":         "name-asset",
								"name":       "fix-pr",
								"slug":       "another-slug",
							},
							map[string]any{
								"__typename": "Skill",
								"id":         "slug-asset",
								"name":       "Fix PR",
								"slug":       "fix-pr",
							},
						},
					},
				},
			}
		},
		"InstallSkillToBot": func(vars map[string]any) any {
			if got := vars["skillId"]; got != "slug-asset" {
				t.Fatalf("InstallSkillToBot skillId = %v, want slug-asset", got)
			}
			return map[string]any{
				"installSkillToBot": map[string]any{
					"success": true,
					"errors":  []any{},
				},
			}
		},
	})

	v := NewSleuthVault(srv.URL, "test-token")
	if err := v.SetAssetInstallation(context.Background(), "fix-pr", InstallTarget{Kind: InstallKindBot, Bot: "testers"}); err != nil {
		t.Fatalf("SetAssetInstallation: %v", err)
	}
}

func TestSleuthVault_ClearAssetInstallationsIgnoresMissingBotInstall(t *testing.T) {
	srv, records := mockSleuthGraphQL(t, map[string]func(map[string]any) any{
		"ListBots": func(vars map[string]any) any {
			return map[string]any{
				"bots": []any{
					map[string]any{
						"id":              "bot-1",
						"name":            "testers",
						"slug":            "testers",
						"description":     "Tests stuff",
						"teams":           []any{},
						"installedSkills": []any{},
					},
				},
			}
		},
		"BotInstalled": func(vars map[string]any) any {
			if got := vars["slug"]; got != "testers" {
				t.Fatalf("BotInstalled slug = %v, want testers", got)
			}
			return map[string]any{
				"bot": map[string]any{
					"installedSkills": []any{
						map[string]any{
							"name":            "database-migrations",
							"assetType":       "SKILL",
							"isDirectInstall": true,
						},
					},
				},
			}
		},
		"AssetGID": func(vars map[string]any) any {
			if got := vars["search"]; got != "database-migrations" {
				t.Fatalf("AssetGID search = %v, want database-migrations", got)
			}
			return map[string]any{
				"vault": map[string]any{
					"assets": map[string]any{
						"nodes": []any{
							map[string]any{
								"__typename": "Skill",
								"id":         "skill-1",
								"name":       "Database migrations",
								"slug":       "database-migrations",
								"type":       "SKILL",
							},
						},
					},
				},
			}
		},
		"UninstallSkillFromBot": func(vars map[string]any) any {
			if got := vars["botId"]; got != "bot-1" {
				t.Fatalf("UninstallSkillFromBot botId = %v, want bot-1", got)
			}
			if got := vars["skillId"]; got != "skill-1" {
				t.Fatalf("UninstallSkillFromBot skillId = %v, want skill-1", got)
			}
			return map[string]any{
				"uninstallSkillFromBot": map[string]any{
					"success": false,
					"errors":  []any{},
				},
			}
		},
		"RemoveAssetInstallations": func(vars map[string]any) any {
			input, ok := vars["input"].(map[string]any)
			if !ok {
				t.Fatalf("RemoveAssetInstallations input = %T, want object", vars["input"])
			}
			if got := input["assetName"]; got != "database-migrations" {
				t.Fatalf("RemoveAssetInstallations assetName = %v, want database-migrations", got)
			}
			return map[string]any{
				"removeAssetInstallations": map[string]any{
					"success": true,
					"errors":  []any{},
				},
			}
		},
	})

	v := NewSleuthVault(srv.URL, "test-token")
	if err := v.ClearAssetInstallations(context.Background(), "database-migrations"); err != nil {
		t.Fatalf("ClearAssetInstallations failed: %v", err)
	}

	var ops []string
	for _, rec := range *records {
		ops = append(ops, rec.OperationName)
	}
	if got := strings.Join(ops, ","); got != "AssetGID,ListBots,BotInstalled,UninstallSkillFromBot,RemoveAssetInstallations" {
		t.Fatalf("operations = %s", got)
	}
}

func TestSleuthVault_ClearAssetInstallationsFiltersBotInstallsByAssetType(t *testing.T) {
	srv, records := mockSleuthGraphQL(t, map[string]func(map[string]any) any{
		"AssetGID": func(vars map[string]any) any {
			return map[string]any{
				"vault": map[string]any{
					"assets": map[string]any{
						"nodes": []any{
							map[string]any{
								"__typename": "Skill",
								"id":         "skill-1",
								"name":       "Foo",
								"slug":       "foo",
								"type":       "SKILL",
							},
						},
					},
				},
			}
		},
		"ListBots": func(vars map[string]any) any {
			return map[string]any{
				"bots": []any{
					map[string]any{
						"id":              "bot-1",
						"name":            "testers",
						"slug":            "testers",
						"description":     "Tests stuff",
						"teams":           []any{},
						"installedSkills": []any{},
					},
				},
			}
		},
		"BotInstalled": func(vars map[string]any) any {
			return map[string]any{
				"bot": map[string]any{
					"installedSkills": []any{
						map[string]any{
							"name":            "foo",
							"assetType":       "AGENT",
							"isDirectInstall": true,
						},
					},
				},
			}
		},
		"UninstallSkillFromBot": func(vars map[string]any) any {
			t.Fatal("UninstallSkillFromBot should not run for a different asset type")
			return nil
		},
		"RemoveAssetInstallations": func(vars map[string]any) any {
			return map[string]any{
				"removeAssetInstallations": map[string]any{
					"success": true,
					"errors":  []any{},
				},
			}
		},
	})

	v := NewSleuthVault(srv.URL, "test-token")
	if err := v.ClearAssetInstallations(context.Background(), "foo"); err != nil {
		t.Fatalf("ClearAssetInstallations failed: %v", err)
	}

	var ops []string
	for _, rec := range *records {
		ops = append(ops, rec.OperationName)
	}
	if got := strings.Join(ops, ","); got != "AssetGID,ListBots,BotInstalled,RemoveAssetInstallations" {
		t.Fatalf("operations = %s", got)
	}
}

// TestSleuthVault_AddAsset_ConflictByStatusCarriesSlug verifies that an
// HTTP 409 is treated as a version conflict even when the error message is
// reworded (not containing "already exists"), and that the persisted slug
// from the response propagates onto ErrVersionExists.
func TestSleuthVault_AddAsset_ConflictByStatusCarriesSlug(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/skills/assets" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": false,
			"error":   "duplicate version", // reworded — no "already exists"
			"asset":   map[string]any{"name": "foo_skill", "version": "1"},
		})
	}))
	t.Cleanup(srv.Close)

	// Server contract: this pins only the SX side — that we PARSE the
	// conflict response's asset.name as the slug. The server must ALSO
	// guarantee that field carries the persisted slug ("foo_skill"), not the
	// display name ("foo"); if it ever returns the display name on conflict,
	// the upload-collision re-publish fix silently regresses and this test
	// would not catch it.
	v := NewSleuthVault(srv.URL, "test-token")
	_, err := v.AddAssetWithResult(context.Background(), &lockfile.Asset{Name: "foo", Version: "1"}, []byte("zip"))
	var exists *ErrVersionExists
	if !errors.As(err, &exists) {
		t.Fatalf("AddAssetWithResult err = %v, want ErrVersionExists", err)
	}
	if exists.Slug != "foo_skill" {
		t.Fatalf("ErrVersionExists.Slug = %q, want foo_skill", exists.Slug)
	}
}

func TestSleuthVault_AddAssetHTTPErrorIncludesBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/skills/assets" {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "agent install exploded", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	v := NewSleuthVault(srv.URL, "test-token")
	_, err := v.AddAssetWithResult(context.Background(), &lockfile.Asset{Name: "foo", Version: "1"}, []byte("zip"))
	if err == nil || !strings.Contains(err.Error(), "HTTP 500:") || !strings.Contains(err.Error(), "agent install exploded") {
		t.Fatalf("AddAssetWithResult err = %v, want status plus response body", err)
	}
}

// TestSleuthVault_InstallSkillToBot_AmbiguousNameErrors covers the case
// where no asset matches the requested value as a slug but two distinct
// assets share it as a display name. The resolver must surface an
// ambiguity error rather than silently installing the first one.
func TestSleuthVault_InstallSkillToBot_AmbiguousNameErrors(t *testing.T) {
	srv, _ := mockSleuthGraphQL(t, map[string]func(map[string]any) any{
		"ListBots": func(vars map[string]any) any {
			return map[string]any{
				"bots": []any{
					map[string]any{
						"id":          "bot-1",
						"name":        "testers",
						"slug":        "testers",
						"description": "Tests stuff",
						"teams":       []any{},
					},
				},
			}
		},
		"AssetGID": func(vars map[string]any) any {
			return map[string]any{
				"vault": map[string]any{
					"assets": map[string]any{
						"nodes": []any{
							map[string]any{
								"__typename": "Skill",
								"id":         "asset-a",
								"name":       "fix-pr",
								"slug":       "fix-pr-one",
							},
							map[string]any{
								"__typename": "Skill",
								"id":         "asset-b",
								"name":       "fix-pr",
								"slug":       "fix-pr-two",
							},
						},
					},
				},
			}
		},
		"InstallSkillToBot": func(vars map[string]any) any {
			t.Fatal("InstallSkillToBot should not be called on an ambiguous name")
			return nil
		},
	})

	v := NewSleuthVault(srv.URL, "test-token")
	err := v.SetAssetInstallation(context.Background(), "fix-pr", InstallTarget{Kind: InstallKindBot, Bot: "testers"})
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("SetAssetInstallation with ambiguous name = %v, want ambiguity error", err)
	}
}

// TestSleuthVault_SetInstallations_OmitsEmptyVersion verifies that the
// SetAssetInstallations mutation omits assetVersion when asset.Version is
// "" (the optional field must not be sent as the empty string). Also
// verifies the inverse: a populated version is sent.
func TestSleuthVault_SetInstallations_OmitsEmptyVersion(t *testing.T) {
	tests := []struct {
		name           string
		version        string
		wantAssetVer   bool
		wantVersionStr string
	}{
		{name: "empty version is omitted", version: "", wantAssetVer: false},
		{name: "populated version is sent", version: "1.2.3", wantAssetVer: true, wantVersionStr: "1.2.3"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, records := mockSleuthGraphQL(t, map[string]func(map[string]any) any{
				"SetAssetInstallations": func(vars map[string]any) any {
					return map[string]any{
						"setAssetInstallations": map[string]any{
							"success": true,
							"errors":  []any{},
						},
					}
				},
			})

			v := NewSleuthVault(srv.URL, "test-token")
			a := &lockfile.Asset{
				Name:    "my-skill",
				Version: tc.version,
				Scopes:  nil, // global install
			}
			if err := v.SetInstallations(context.Background(), a, ""); err != nil {
				t.Fatalf("SetInstallations failed: %v", err)
			}
			if len(*records) != 1 {
				t.Fatalf("expected 1 GraphQL request, got %d", len(*records))
			}
			rec := (*records)[0]
			input, _ := rec.Variables["input"].(map[string]any)
			gotVer, hasVer := input["assetVersion"]
			if tc.wantAssetVer {
				if !hasVer {
					t.Fatalf("expected assetVersion in input, got: %+v", input)
				}
				if got, _ := gotVer.(string); got != tc.wantVersionStr {
					t.Errorf("assetVersion=%q, want %q", got, tc.wantVersionStr)
				}
			} else {
				// The fix's contract: when asset.Version is empty, the
				// wire must NOT carry "assetVersion":"" — that would tell
				// the server to set the version to the empty string. nil
				// (JSON null) is acceptable; it means "unset".
				if hasVer && gotVer != nil {
					t.Errorf("assetVersion should be absent or null, got %v (raw: %s)", gotVer, rec.RawBody)
				}
				if strings.Contains(rec.RawBody, `"assetVersion":""`) {
					t.Errorf("raw body must not send assetVersion as empty string: %s", rec.RawBody)
				}
			}
		})
	}
}

func makeTeamNodes(count int, prefix string) []any {
	nodes := make([]any, count)
	for i := range count {
		nodes[i] = map[string]any{
			"id":                 fmt.Sprintf("team-%d", i),
			"name":               fmt.Sprintf("%s-%d", prefix, i),
			"adminMembers":       []any{},
			"members":            map[string]any{"totalCount": 0, "nodes": []any{}},
			"skillsRepositories": []any{},
		}
	}
	return nodes
}

func TestSleuthVault_ListTeams_DefaultLimitAndTotalCount(t *testing.T) {
	srv, _ := mockSleuthGraphQL(t, map[string]func(map[string]any) any{
		"ListTeams": func(vars map[string]any) any {
			first, _ := vars["first"].(float64)
			if int(first) > 30 {
				t.Errorf("expected first <= 30, got %v", first)
			}
			return map[string]any{
				"organization": map[string]any{
					"teams": map[string]any{
						"nodes":      makeTeamNodes(20, "team"),
						"totalCount": 55,
						"pageInfo": map[string]any{
							"hasNextPage": true,
							"endCursor":   "cursor-20",
						},
					},
				},
			}
		},
	})

	v := NewSleuthVault(srv.URL, "test-token")
	result, err := v.ListTeams(context.Background(), ListTeamsOptions{})
	if err != nil {
		t.Fatalf("ListTeams failed: %v", err)
	}
	if len(result.Teams) != 20 {
		t.Errorf("expected 20 teams, got %d", len(result.Teams))
	}
	if result.TotalCount != 55 {
		t.Errorf("expected TotalCount=55, got %d", result.TotalCount)
	}
	if !result.HasMore {
		t.Error("expected HasMore=true")
	}
}

func TestSleuthVault_ListTeams_FilterPassesTerm(t *testing.T) {
	srv, records := mockSleuthGraphQL(t, map[string]func(map[string]any) any{
		"ListTeams": func(vars map[string]any) any {
			term, _ := vars["term"].(string)
			if term != "platform" {
				t.Errorf("expected term=platform, got %q", term)
			}
			return map[string]any{
				"organization": map[string]any{
					"teams": map[string]any{
						"nodes":      makeTeamNodes(2, "platform"),
						"totalCount": 2,
						"pageInfo": map[string]any{
							"hasNextPage": false,
							"endCursor":   nil,
						},
					},
				},
			}
		},
	})

	v := NewSleuthVault(srv.URL, "test-token")
	result, err := v.ListTeams(context.Background(), ListTeamsOptions{Filter: "platform"})
	if err != nil {
		t.Fatalf("ListTeams failed: %v", err)
	}
	if len(result.Teams) != 2 {
		t.Errorf("expected 2 teams, got %d", len(result.Teams))
	}
	if result.TotalCount != 2 {
		t.Errorf("expected TotalCount=2, got %d", result.TotalCount)
	}
	if result.HasMore {
		t.Error("expected HasMore=false")
	}
	rec := (*records)[0]
	if _, ok := rec.Variables["term"]; !ok {
		t.Error("expected $term variable in request")
	}
}

func TestSleuthVault_CreateTeam_SetsAdminsAfterCreation(t *testing.T) {
	srv, records := mockSleuthGraphQL(t, map[string]func(map[string]any) any{
		"FindUser": func(vars map[string]any) any {
			term := vars["term"].(string)
			return map[string]any{
				"organization": map[string]any{
					"users": map[string]any{
						"nodes": []any{
							map[string]any{"id": "u-" + term, "email": term},
						},
					},
				},
			}
		},
		"CreateTeam": func(vars map[string]any) any {
			return map[string]any{
				"createTeam": map[string]any{
					"team":   map[string]any{"id": "team-new", "name": "platform"},
					"errors": []any{},
				},
			}
		},
		"ListTeams": func(vars map[string]any) any {
			return map[string]any{
				"organization": map[string]any{
					"teams": map[string]any{
						"nodes": []any{
							map[string]any{
								"id":                 "team-new",
								"name":               "platform",
								"adminMembers":       []any{},
								"members":            map[string]any{"totalCount": 0, "nodes": []any{}},
								"skillsRepositories": []any{},
							},
						},
						"totalCount": 1,
						"pageInfo":   map[string]any{"hasNextPage": false, "endCursor": nil},
					},
				},
			}
		},
		"SetTeamAdmin": func(vars map[string]any) any {
			return map[string]any{
				"setTeamAdmin": map[string]any{
					"team":   map[string]any{"id": "team-new"},
					"errors": []any{},
				},
			}
		},
	})

	v := NewSleuthVault(srv.URL, "test-token")
	err := v.CreateTeam(context.Background(), mgmt.Team{
		Name:    "platform",
		Members: []string{"alice@example.com", "bob@example.com"},
		Admins:  []string{"alice@example.com", "bob@example.com"},
	})
	if err != nil {
		t.Fatalf("CreateTeam failed: %v", err)
	}

	var ops []string
	for _, r := range *records {
		ops = append(ops, r.OperationName)
	}
	// Expect: FindUser(alice), FindUser(bob), CreateTeam, ListTeams(for teamGID), SetTeamAdmin(alice), ListTeams, SetTeamAdmin(bob)
	createIdx := -1
	setAdminCount := 0
	for i, op := range ops {
		if op == "CreateTeam" {
			createIdx = i
		}
		if op == "SetTeamAdmin" {
			setAdminCount++
			if createIdx < 0 {
				t.Fatal("SetTeamAdmin called before CreateTeam")
			}
		}
	}
	if createIdx < 0 {
		t.Fatal("CreateTeam was never called")
	}
	if setAdminCount != 2 {
		t.Errorf("expected 2 SetTeamAdmin calls, got %d; ops=%v", setAdminCount, ops)
	}
}

func TestSleuthVault_CreateTeam_NoAdminsSkipsSetTeamAdmin(t *testing.T) {
	srv, records := mockSleuthGraphQL(t, map[string]func(map[string]any) any{
		"FindUser": func(vars map[string]any) any {
			term := vars["term"].(string)
			return map[string]any{
				"organization": map[string]any{
					"users": map[string]any{
						"nodes": []any{
							map[string]any{"id": "u-" + term, "email": term},
						},
					},
				},
			}
		},
		"CreateTeam": func(vars map[string]any) any {
			return map[string]any{
				"createTeam": map[string]any{
					"team":   map[string]any{"id": "team-new", "name": "solo"},
					"errors": []any{},
				},
			}
		},
	})

	v := NewSleuthVault(srv.URL, "test-token")
	err := v.CreateTeam(context.Background(), mgmt.Team{
		Name:    "solo",
		Members: []string{"alice@example.com"},
	})
	if err != nil {
		t.Fatalf("CreateTeam failed: %v", err)
	}

	for _, r := range *records {
		if r.OperationName == "SetTeamAdmin" {
			t.Fatal("SetTeamAdmin should not be called when no admins specified")
		}
	}
}
