package vault

import (
	"context"
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
	if !v.SupportsAppPlugins(context.Background()) {
		t.Fatalf("probe reported unsupported against a serving backend")
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
