package vault

import (
	"context"
	"testing"

	"github.com/sleuth-io/sx/internal/manifest"
	"github.com/sleuth-io/sx/internal/mgmt"
)

func appPluginTestVault(t *testing.T, orgAdmins []string) *PathVault {
	t.Helper()
	mgmt.ResetActorCache()
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "alice@example.com")
	runGit(t, dir, "config", "user.name", "Alice")
	m := &manifest.Manifest{SchemaVersion: manifest.CurrentSchemaVersion}
	if len(orgAdmins) > 0 {
		m.Org = &manifest.Org{Admins: orgAdmins}
	}
	if err := manifest.Save(dir, m); err != nil {
		t.Fatalf("seed manifest: %v", err)
	}
	v, err := NewPathVault("file://" + dir)
	if err != nil {
		t.Fatalf("NewPathVault: %v", err)
	}
	return v
}

// Policy round-trips through the manifest; nil clears back to open.
func TestAppPluginPolicyRoundTrip(t *testing.T) {
	v := appPluginTestVault(t, nil)
	ctx := context.Background()

	policy, err := v.AppPluginPolicy(ctx)
	if err != nil || policy != nil {
		t.Fatalf("initial policy = %+v, %v; want nil (open)", policy, err)
	}

	want := &manifest.AppPluginPolicy{
		Mode:    AppPluginModeAllowlist,
		Allowed: []string{"publish-doctor", "library-dashboard", "publish-doctor"},
	}
	if err := v.SetAppPluginPolicy(ctx, want); err != nil {
		t.Fatalf("set: %v", err)
	}
	policy, err = v.AppPluginPolicy(ctx)
	if err != nil || policy == nil || policy.Mode != AppPluginModeAllowlist {
		t.Fatalf("policy = %+v, %v", policy, err)
	}
	// Allowed list is sorted + deduped on write.
	if len(policy.Allowed) != 2 || policy.Allowed[0] != "library-dashboard" {
		t.Fatalf("allowed = %v, want sorted dedup", policy.Allowed)
	}

	if err := v.SetAppPluginPolicy(ctx, &manifest.AppPluginPolicy{Mode: "bogus"}); err == nil {
		t.Fatalf("invalid mode accepted")
	}
	if err := v.SetAppPluginPolicy(ctx, nil); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if policy, _ := v.AppPluginPolicy(ctx); policy != nil {
		t.Fatalf("policy not cleared: %+v", policy)
	}
}

// On a governed vault only org-admins may change the policy.
func TestAppPluginPolicyRBAC(t *testing.T) {
	v := appPluginTestVault(t, []string{"someone-else@example.com"})
	err := v.SetAppPluginPolicy(context.Background(), &manifest.AppPluginPolicy{Mode: AppPluginModeDisabled})
	if err == nil {
		t.Fatalf("non-admin policy write accepted on governed vault")
	}
}
