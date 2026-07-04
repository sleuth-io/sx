package vault

import (
	"context"
	"errors"
	"maps"
	"reflect"
	"strings"
	"testing"

	"github.com/sleuth-io/sx/internal/manifest"
)

func collectionsPage(nodes ...map[string]any) map[string]any {
	if nodes == nil {
		nodes = []map[string]any{}
	}
	return map[string]any{
		"collections": map[string]any{
			"pageInfo": map[string]any{"hasNextPage": false, "endCursor": nil},
			"nodes":    nodes,
		},
	}
}

func collectionNode(gid, name, description string) map[string]any {
	return map[string]any{
		"id":          gid,
		"name":        name,
		"description": description,
	}
}

func collectionAssetsPage(assets ...map[string]any) map[string]any {
	if assets == nil {
		assets = []map[string]any{}
	}
	return map[string]any{
		"collection": map[string]any{
			"assets": map[string]any{
				"pageInfo": map[string]any{"hasNextPage": false, "endCursor": nil},
				"nodes":    assets,
			},
		},
	}
}

func assetNode(gid, slug string) map[string]any {
	return map[string]any{"id": gid, "slug": slug, "__typename": "Skill"}
}

func assetGIDPage(nodes ...map[string]any) map[string]any {
	withNames := make([]map[string]any, 0, len(nodes))
	for _, n := range nodes {
		full := map[string]any{"name": n["slug"], "type": "SKILL"}
		maps.Copy(full, n)
		withNames = append(withNames, full)
	}
	return map[string]any{
		"vault": map[string]any{
			"assets": map[string]any{
				"pageInfo": map[string]any{"hasNextPage": false, "endCursor": nil},
				"nodes":    withNames,
			},
		},
	}
}

func TestSleuthVault_ListCollections(t *testing.T) {
	srv, _ := mockSleuthGraphQL(t, map[string]func(map[string]any) any{
		"ListCollections": func(map[string]any) any {
			return collectionsPage(
				collectionNode("CL1", "writing", "Writing helpers"),
				collectionNode("CL2", "empty", ""),
			)
		},
		"CollectionAssets": func(vars map[string]any) any {
			if vars["id"] == "CL1" {
				return collectionAssetsPage(assetNode("AS2", "release-notes"), assetNode("AS1", "brand-voice"))
			}
			return collectionAssetsPage()
		},
	})

	v := NewSleuthVault(srv.URL, "token")
	got, err := v.ListCollections(context.Background())
	if err != nil {
		t.Fatalf("ListCollections: %v", err)
	}
	want := []manifest.Collection{
		{Name: "writing", Description: "Writing helpers", Assets: []string{"brand-voice", "release-notes"}},
		{Name: "empty", Description: "", Assets: []string{}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListCollections = %+v, want %+v", got, want)
	}
}

func TestSleuthVault_SaveCollection_CreatesWhenMissing(t *testing.T) {
	srv, records := mockSleuthGraphQL(t, map[string]func(map[string]any) any{
		"ListCollections": func(map[string]any) any { return collectionsPage() },
		"AssetGID": func(map[string]any) any {
			return assetGIDPage(assetNode("AS1", "brand-voice"), assetNode("AS2", "release-notes"))
		},
		"CreateAssetCollection": func(map[string]any) any {
			return map[string]any{
				"createAssetCollection": map[string]any{
					"collection": map[string]any{"id": "CL9"},
					"errors":     []any{},
				},
			}
		},
	})

	v := NewSleuthVault(srv.URL, "token")
	err := v.SaveCollection(context.Background(), manifest.Collection{
		Name:        "writing",
		Description: "Writing helpers",
		Assets:      []string{"brand-voice", "release-notes"},
	})
	if err != nil {
		t.Fatalf("SaveCollection: %v", err)
	}

	create := findRecord(t, *records, "CreateAssetCollection")
	input := create.Variables["input"].(map[string]any)
	if input["name"] != "writing" || input["description"] != "Writing helpers" {
		t.Fatalf("create input = %+v", input)
	}
	gids := input["assetGids"].([]any)
	if len(gids) != 2 || gids[0] != "AS1" || gids[1] != "AS2" {
		t.Fatalf("assetGids = %v, want [AS1 AS2]", gids)
	}
}

func TestSleuthVault_SaveCollection_DiffsMembershipAndDescription(t *testing.T) {
	srv, records := mockSleuthGraphQL(t, map[string]func(map[string]any) any{
		"ListCollections": func(map[string]any) any {
			return collectionsPage(collectionNode("CL1", "writing", "old"))
		},
		"CollectionAssets": func(map[string]any) any {
			return collectionAssetsPage(assetNode("AS1", "brand-voice"), assetNode("AS2", "release-notes"))
		},
		"AssetGID": func(map[string]any) any {
			return assetGIDPage(
				assetNode("AS2", "release-notes"), assetNode("AS3", "pr-checklist"))
		},
		"UpdateAssetCollection": func(map[string]any) any {
			return map[string]any{
				"updateAssetCollection": map[string]any{
					"collection": map[string]any{"id": "CL1"},
					"errors":     []any{},
				},
			}
		},
		"AddAssetsToCollection": func(map[string]any) any {
			return map[string]any{
				"addAssetsToCollection": map[string]any{
					"collection": map[string]any{"id": "CL1"},
					"errors":     []any{},
				},
			}
		},
		"RemoveAssetsFromCollection": func(map[string]any) any {
			return map[string]any{
				"removeAssetsFromCollection": map[string]any{
					"collection": map[string]any{"id": "CL1"},
					"errors":     []any{},
				},
			}
		},
	})

	v := NewSleuthVault(srv.URL, "token")
	err := v.SaveCollection(context.Background(), manifest.Collection{
		Name:        "writing",
		Description: "new",
		Assets:      []string{"release-notes", "pr-checklist"},
	})
	if err != nil {
		t.Fatalf("SaveCollection: %v", err)
	}

	update := findRecord(t, *records, "UpdateAssetCollection")
	if update.Variables["input"].(map[string]any)["description"] != "new" {
		t.Fatalf("update input = %+v", update.Variables)
	}
	add := findRecord(t, *records, "AddAssetsToCollection")
	if gids := add.Variables["input"].(map[string]any)["assetGids"].([]any); len(gids) != 1 || gids[0] != "AS3" {
		t.Fatalf("added gids = %v, want [AS3]", gids)
	}
	remove := findRecord(t, *records, "RemoveAssetsFromCollection")
	if gids := remove.Variables["input"].(map[string]any)["assetGids"].([]any); len(gids) != 1 || gids[0] != "AS1" {
		t.Fatalf("removed gids = %v, want [AS1]", gids)
	}
}

func TestSleuthVault_SaveCollection_MissingAssetErrors(t *testing.T) {
	srv, _ := mockSleuthGraphQL(t, map[string]func(map[string]any) any{
		"ListCollections": func(map[string]any) any { return collectionsPage() },
		"AssetGID":        func(map[string]any) any { return assetGIDPage() },
	})

	v := NewSleuthVault(srv.URL, "token")
	err := v.SaveCollection(context.Background(), manifest.Collection{
		Name:   "writing",
		Assets: []string{"nope"},
	})
	if !errors.Is(err, ErrAssetNotFound) {
		t.Fatalf("SaveCollection err = %v, want ErrAssetNotFound", err)
	}
}

func TestSleuthVault_SaveCollection_SurfacesMutationErrors(t *testing.T) {
	srv, _ := mockSleuthGraphQL(t, map[string]func(map[string]any) any{
		"ListCollections": func(map[string]any) any { return collectionsPage() },
		"AssetGID":        func(map[string]any) any { return assetGIDPage() },
		"CreateAssetCollection": func(map[string]any) any {
			return map[string]any{
				"createAssetCollection": map[string]any{
					"collection": nil,
					"errors": []any{map[string]any{
						"field":    "name",
						"messages": []any{"Collection with name 'writing' already exists"},
					}},
				},
			}
		},
	})

	v := NewSleuthVault(srv.URL, "token")
	err := v.SaveCollection(context.Background(), manifest.Collection{Name: "writing"})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("SaveCollection err = %v, want duplicate-name error", err)
	}
}

func TestSleuthVault_DeleteCollection(t *testing.T) {
	srv, records := mockSleuthGraphQL(t, map[string]func(map[string]any) any{
		"ListCollections": func(map[string]any) any {
			return collectionsPage(collectionNode("CL1", "writing", ""))
		},
		"CollectionAssets": func(map[string]any) any { return collectionAssetsPage() },
		"DeleteAssetCollection": func(map[string]any) any {
			return map[string]any{
				"deleteAssetCollection": map[string]any{"success": true, "errors": []any{}},
			}
		},
	})

	v := NewSleuthVault(srv.URL, "token")
	if err := v.DeleteCollection(context.Background(), "writing"); err != nil {
		t.Fatalf("DeleteCollection: %v", err)
	}
	del := findRecord(t, *records, "DeleteAssetCollection")
	if del.Variables["gid"] != "CL1" {
		t.Fatalf("delete gid = %v, want CL1", del.Variables["gid"])
	}

	if err := v.DeleteCollection(context.Background(), "missing"); !errors.Is(err, manifest.ErrCollectionNotFound) {
		t.Fatalf("DeleteCollection(missing) err = %v, want ErrCollectionNotFound", err)
	}
}

func findRecord(t *testing.T, records []sleuthGQLRecord, op string) sleuthGQLRecord {
	t.Helper()
	for _, r := range records {
		if r.OperationName == op {
			return r
		}
	}
	t.Fatalf("no %s request recorded; got %+v", op, records)
	return sleuthGQLRecord{}
}
