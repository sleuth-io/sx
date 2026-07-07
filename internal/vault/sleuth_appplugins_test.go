package vault

import (
	"context"
	"errors"
	"testing"

	"github.com/sleuth-io/sx/internal/manifest"
)

// The policy read maps server enum casing to the manifest's lowercase
// modes, and OPEN normalizes to nil (same contract as file vaults).
func TestSleuthAppPluginPolicyRead(t *testing.T) {
	srv, _ := mockSleuthGraphQL(t, map[string]func(map[string]any) any{
		"GetAppPluginPolicy": func(map[string]any) any {
			return map[string]any{"vault": map[string]any{
				"appPluginPolicy": map[string]any{
					"mode": "ALLOWLIST", "allowed": []string{"review-rota"},
				},
			}}
		},
	})
	v := NewSleuthVault(srv.URL, "test-token")
	policy, err := v.AppPluginPolicy(context.Background())
	if err != nil || policy == nil {
		t.Fatalf("policy = %+v, %v", policy, err)
	}
	if policy.Mode != AppPluginModeAllowlist || len(policy.Allowed) != 1 {
		t.Fatalf("mapped policy = %+v", policy)
	}
	supported, definitive := v.SupportsAppPlugins(context.Background())
	if !supported || !definitive {
		t.Fatalf("probe = %v/%v against a serving backend", supported, definitive)
	}
}

// The schema-unknown classifier is pinned: unknown-field and enum
// rejections are definitive "old server"; anything else is transient.
func TestAppPluginSchemaUnknownErr(t *testing.T) {
	definitive := []string{
		`Cannot query field "appPluginPolicy" on type "VaultGqlType"`,
		`Variable "$assetType" got invalid value "APP_PLUGIN"`,
		`Enum "AssetType" cannot represent value: "APP_PLUGIN"`,
	}
	for _, m := range definitive {
		if !isAppPluginSchemaUnknownErr(errors.New(m)) {
			t.Errorf("%q not classified as schema-unknown", m)
		}
	}
	transient := []string{"returned error 502: bad gateway", "context deadline exceeded", ""}
	for _, m := range transient {
		if isAppPluginSchemaUnknownErr(errors.New(m)) {
			t.Errorf("%q wrongly classified as schema-unknown", m)
		}
	}
	if isAppPluginSchemaUnknownErr(nil) {
		t.Errorf("nil classified as schema-unknown")
	}
}

func TestSleuthAppPluginPolicyOpenIsNil(t *testing.T) {
	srv, _ := mockSleuthGraphQL(t, map[string]func(map[string]any) any{
		"GetAppPluginPolicy": func(map[string]any) any {
			return map[string]any{"vault": map[string]any{
				"appPluginPolicy": map[string]any{"mode": "OPEN", "allowed": []string{}},
			}}
		},
	})
	v := NewSleuthVault(srv.URL, "test-token")
	policy, err := v.AppPluginPolicy(context.Background())
	if err != nil || policy != nil {
		t.Fatalf("open policy = %+v, %v; want nil", policy, err)
	}
}

// The write sends the uppercased enum, drops allowed unless allowlist,
// and surfaces server mutation errors (the org-admin gate) as Go errors.
func TestSleuthAppPluginPolicyWrite(t *testing.T) {
	srv, records := mockSleuthGraphQL(t, map[string]func(map[string]any) any{
		"SetAppPluginPolicy": func(map[string]any) any {
			return map[string]any{"setAppPluginPolicy": map[string]any{
				"appPluginPolicy": map[string]any{"mode": "DISABLED"},
				"errors":          []any{},
			}}
		},
	})
	v := NewSleuthVault(srv.URL, "test-token")
	err := v.SetAppPluginPolicy(context.Background(), &manifest.AppPluginPolicy{Mode: AppPluginModeDisabled})
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	last := (*records)[len(*records)-1]
	input := last.Variables["input"].(map[string]any)
	if input["mode"] != "DISABLED" {
		t.Fatalf("sent mode = %v", input["mode"])
	}
	if v.SetAppPluginPolicy(context.Background(), &manifest.AppPluginPolicy{Mode: "bogus"}) == nil {
		t.Fatalf("invalid mode accepted client-side")
	}
}

func TestSleuthAppPluginPolicyWriteDenied(t *testing.T) {
	srv, _ := mockSleuthGraphQL(t, map[string]func(map[string]any) any{
		"SetAppPluginPolicy": func(map[string]any) any {
			return map[string]any{"setAppPluginPolicy": map[string]any{
				"appPluginPolicy": nil,
				"errors": []any{map[string]any{
					"field": "mode", "messages": []string{"only org admins can change the extension policy"},
				}},
			}}
		},
	})
	v := NewSleuthVault(srv.URL, "test-token")
	err := v.SetAppPluginPolicy(context.Background(), &manifest.AppPluginPolicy{Mode: AppPluginModeDisabled})
	if err == nil {
		t.Fatalf("server denial not surfaced")
	}
}

// Shared storage round-trips through appPluginStorage; null reads as
// unset and empty save sends a delete (nil data).
func TestSleuthAppPluginSharedStorage(t *testing.T) {
	srv, records := mockSleuthGraphQL(t, map[string]func(map[string]any) any{
		"GetAppPluginStorage": func(map[string]any) any {
			return map[string]any{"vault": map[string]any{
				"appPluginStorage": `{"assets":{}}`,
			}}
		},
		"SetAppPluginStorage": func(map[string]any) any {
			return map[string]any{"setAppPluginStorage": map[string]any{"errors": []any{}}}
		},
	})
	v := NewSleuthVault(srv.URL, "test-token")
	ctx := context.Background()
	got, err := v.AppPluginSharedLoad(ctx, "review-rota")
	if err != nil || got != `{"assets":{}}` {
		t.Fatalf("load = %q, %v", got, err)
	}
	if err := v.AppPluginSharedSave(ctx, "review-rota", `{"assets":{"a":1}}`); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := v.AppPluginSharedSave(ctx, "review-rota", ""); err != nil {
		t.Fatalf("delete: %v", err)
	}
	last := (*records)[len(*records)-1]
	input := last.Variables["input"].(map[string]any)
	if input["data"] != nil {
		t.Fatalf("delete sent data = %v, want null", input["data"])
	}
}
