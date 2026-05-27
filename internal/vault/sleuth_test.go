package vault

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
