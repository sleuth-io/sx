package vault

import (
	"context"
	"strings"
	"testing"
)

// TestOrgAdminBootstrap: while the list is empty anyone may seed the first
// org-admin; once set, a non-admin may not change it.
func TestOrgAdminBootstrap(t *testing.T) {
	ctx := context.Background()
	v := seedRBACVault(t, "mallory@example.com", nil, nil)

	if n, err := v.AddOrgAdmins(ctx, []string{"boss@example.com"}); err != nil || n != 1 {
		t.Fatalf("bootstrap add by anyone: n=%d err=%v", n, err)
	}
	admins, err := v.ListOrgAdmins(ctx)
	if err != nil || len(admins) != 1 || admins[0] != "boss@example.com" {
		t.Fatalf("list after bootstrap: %+v err=%v", admins, err)
	}
	// Now governed: mallory (not an admin) cannot change the list.
	if _, err := v.AddOrgAdmins(ctx, []string{"mallory@example.com"}); err == nil ||
		!strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("non-admin should not change org-admins, got %v", err)
	}
}

// TestOrgAdminRemove: an org-admin can remove admins; removing the last one
// returns the vault to ungoverned.
func TestOrgAdminRemove(t *testing.T) {
	ctx := context.Background()
	v := seedRBACVault(t, "boss@example.com", nil, []string{"boss@example.com"})
	if err := v.RemoveOrgAdmin(ctx, "boss@example.com"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	admins, err := v.ListOrgAdmins(ctx)
	if err != nil || len(admins) != 0 {
		t.Fatalf("expected ungoverned after removing last admin, got %+v err=%v", admins, err)
	}
}

// TestOrgAdmin_Scenarios covers org-admin list management edge cases and gating.
func TestOrgAdmin_Scenarios(t *testing.T) {
	ctx := context.Background()

	t.Run("add accepts and dedupes multiple emails", func(t *testing.T) {
		v := seedRBACVault(t, "boss@example.com", nil, nil)
		n, err := v.AddOrgAdmins(ctx, []string{"boss@example.com", "ann@example.com", "boss@example.com"})
		if err != nil || n != 2 {
			t.Fatalf("add multiple: n=%d err=%v", n, err)
		}
	})

	t.Run("adding an existing admin is a no-op", func(t *testing.T) {
		v := seedRBACVault(t, "boss@example.com", nil, []string{"boss@example.com"})
		n, err := v.AddOrgAdmins(ctx, []string{"boss@example.com"})
		if err != nil || n != 0 {
			t.Fatalf("idempotent add: n=%d err=%v", n, err)
		}
	})

	t.Run("non-admin cannot remove", func(t *testing.T) {
		v := seedRBACVault(t, "mallory@example.com", nil, []string{"boss@example.com"})
		if err := v.RemoveOrgAdmin(ctx, "boss@example.com"); err == nil ||
			!strings.Contains(err.Error(), "permission denied") {
			t.Fatalf("non-admin remove should be denied, got %v", err)
		}
	})

	t.Run("removing a non-admin errors", func(t *testing.T) {
		v := seedRBACVault(t, "boss@example.com", nil, []string{"boss@example.com"})
		if err := v.RemoveOrgAdmin(ctx, "ghost@example.com"); err == nil ||
			!strings.Contains(err.Error(), "not an org-admin") {
			t.Fatalf("removing non-admin should error, got %v", err)
		}
	})
}
