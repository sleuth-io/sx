package vault

import (
	"context"
	"testing"
)

func sleuthTeamsPage(nodes ...map[string]any) map[string]any {
	return map[string]any{
		"organization": map[string]any{
			"teams": map[string]any{
				"nodes":      nodes,
				"totalCount": len(nodes),
				"pageInfo":   map[string]any{"hasNextPage": false, "endCursor": nil},
			},
		},
	}
}

func sleuthTeamNodeFixture(gid, name string) map[string]any {
	return map[string]any{
		"id":           gid,
		"name":         name,
		"adminMembers": []any{},
		"members": map[string]any{
			"totalCount": 0,
			"nodes":      []any{},
		},
		"skillsRepositories": []any{},
	}
}

// Installing a collection to a team sends ONE installCollection mutation
// with the collection GID and the team's GID — never a per-asset fan-out.
func TestSleuthVault_SetCollectionInstallation_Team(t *testing.T) {
	srv, records := mockSleuthGraphQL(t, map[string]func(map[string]any) any{
		"ListCollections": func(map[string]any) any {
			return collectionsPage(collectionNode("CL1", "essentials", ""))
		},
		"ListTeams": func(map[string]any) any {
			return sleuthTeamsPage(sleuthTeamNodeFixture("TM1", "platform"))
		},
		"InstallCollection": func(map[string]any) any {
			return map[string]any{
				"installCollection": map[string]any{
					"collection": map[string]any{"id": "CL1"},
					"errors":     []any{},
				},
			}
		},
	})

	v := NewSleuthVault(srv.URL, "token")
	err := v.SetCollectionInstallation(context.Background(), "essentials", InstallTarget{Kind: InstallKindTeam, Team: "platform"})
	if err != nil {
		t.Fatalf("SetCollectionInstallation: %v", err)
	}

	install := findRecord(t, *records, "InstallCollection")
	input := install.Variables["input"].(map[string]any)
	if input["gid"] != "CL1" {
		t.Fatalf("gid = %v, want CL1", input["gid"])
	}
	insts := input["installations"].([]any)
	if len(insts) != 1 {
		t.Fatalf("installations = %v, want exactly one target", insts)
	}
	first := insts[0].(map[string]any)
	if first["entityType"] != "TEAM" || first["entityId"] != "TM1" {
		t.Fatalf("installation = %+v, want TEAM/TM1", first)
	}
	// No per-asset install mutation may fire.
	for _, r := range *records {
		if r.OperationName == "SetAssetInstallations" {
			t.Fatalf("collection install fanned out to a per-asset mutation")
		}
	}
}

// Uninstalling removes the collection's own row for the target.
func TestSleuthVault_RemoveCollectionInstallation_Org(t *testing.T) {
	srv, records := mockSleuthGraphQL(t, map[string]func(map[string]any) any{
		"ListCollections": func(map[string]any) any {
			return collectionsPage(collectionNode("CL1", "essentials", ""))
		},
		"UninstallCollection": func(map[string]any) any {
			return map[string]any{
				"uninstallCollection": map[string]any{
					"collection": map[string]any{"id": "CL1"},
					"errors":     []any{},
				},
			}
		},
	})

	v := NewSleuthVault(srv.URL, "token")
	err := v.RemoveCollectionInstallation(context.Background(), "essentials", InstallTarget{Kind: InstallKindOrg})
	if err != nil {
		t.Fatalf("RemoveCollectionInstallation: %v", err)
	}
	uninstall := findRecord(t, *records, "UninstallCollection")
	input := uninstall.Variables["input"].(map[string]any)
	if input["gid"] != "CL1" || input["entityType"] != "ORGANIZATION" {
		t.Fatalf("input = %+v, want CL1/ORGANIZATION", input)
	}
}

// The collection's own rows are read as kind-aware targets; org is an
// explicit target (collections with no rows grant nothing).
func TestSleuthVault_CurrentCollectionInstallTargets(t *testing.T) {
	srv, _ := mockSleuthGraphQL(t, map[string]func(map[string]any) any{
		"ListCollections": func(map[string]any) any {
			return collectionsPage(collectionNode("CL1", "essentials", ""))
		},
		"CollectionInstallations": func(vars map[string]any) any {
			if vars["id"] != "CL1" {
				t.Fatalf("CollectionInstallations id = %v, want CL1", vars["id"])
			}
			return map[string]any{
				"collection": map[string]any{
					"installations": []any{
						map[string]any{
							"entityType": "ORGANIZATION", "entityName": "",
							"entityRef": nil, "entityId": nil, "monoRepoConfigId": nil,
						},
						map[string]any{
							"entityType": "TEAM", "entityName": "platform",
							"entityRef": nil, "entityId": "TM1", "monoRepoConfigId": nil,
						},
					},
				},
			}
		},
	})

	v := NewSleuthVault(srv.URL, "token")
	targets, present, err := v.CurrentCollectionInstallTargets(context.Background(), "essentials")
	if err != nil || !present {
		t.Fatalf("CurrentCollectionInstallTargets: present=%v err=%v", present, err)
	}
	if len(targets) != 2 {
		t.Fatalf("targets = %+v, want org + team", targets)
	}
	if targets[0].Kind != InstallKindOrg {
		t.Fatalf("targets[0] = %+v, want org", targets[0])
	}
	if targets[1].Kind != InstallKindTeam || targets[1].Team != "platform" || targets[1].EntityID != "TM1" {
		t.Fatalf("targets[1] = %+v, want team platform/TM1", targets[1])
	}

	// Unknown collection reports not-present, not an error.
	_, present, err = v.CurrentCollectionInstallTargets(context.Background(), "missing")
	if err != nil || present {
		t.Fatalf("missing collection: present=%v err=%v, want false/nil", present, err)
	}
}
