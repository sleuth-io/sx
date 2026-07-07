package vault

import (
	"context"
	"strings"

	"github.com/sleuth-io/sx/internal/manifest"
	vaultgql "github.com/sleuth-io/sx/internal/vault/graphql"
)

// Sleuth-vault extension support (the spec's P5): the server stores
// app-plugin assets through the normal asset pipeline and carries the
// org's extension policy; sx reads and writes it through GraphQL. The
// server enforces the org-admin gate and appends the policy audit event
// itself, so unlike the file vaults there is no client-side RBAC here.
//
// Shared extension storage (storage:shared, API 1.5.0) has no server
// twin yet — SleuthVault deliberately does NOT implement
// AppPluginSharedStore, so the app degrades with its clear
// "can't store shared extension data" error instead of half-working.

// SupportsAppPlugins probes whether the connected server knows the
// app-plugin surface at all — deployed servers predating it reject both
// the asset type and the policy query. The policy read doubles as the
// capability check: unknown-field errors mean "not yet".
func (s *SleuthVault) SupportsAppPlugins(ctx context.Context) bool {
	_, err := vaultgql.GetAppPluginPolicy(ctx, s.gqlClient())
	return err == nil
}

// AppPluginPolicy returns the org's extension policy (nil = open).
func (s *SleuthVault) AppPluginPolicy(ctx context.Context) (*manifest.AppPluginPolicy, error) {
	resp, err := vaultgql.GetAppPluginPolicy(ctx, s.gqlClient())
	if err != nil {
		return nil, err
	}
	p := resp.Vault.AppPluginPolicy
	mode := strings.ToLower(string(p.Mode))
	if mode == "" || mode == AppPluginModeOpen {
		// nil policy IS the open state — the AppPluginPolicyStore
		// contract, matching the file vaults' absent manifest table.
		return nil, nil //nolint:nilnil
	}
	return &manifest.AppPluginPolicy{
		Mode:    mode,
		Allowed: p.Allowed,
	}, nil
}

// SetAppPluginPolicy replaces the org's extension policy. A nil policy
// clears back to open. The server enforces org-admin RBAC and audits.
func (s *SleuthVault) SetAppPluginPolicy(ctx context.Context, policy *manifest.AppPluginPolicy) error {
	if err := validateAppPluginPolicy(policy); err != nil {
		return err
	}
	input := vaultgql.SetAppPluginPolicyInput{
		Mode: vaultgql.AppPluginPolicyModeOpen,
	}
	if policy != nil {
		input.Mode = vaultgql.AppPluginPolicyMode(strings.ToUpper(policy.Mode))
		if policy.Mode == AppPluginModeAllowlist {
			input.Allowed = policy.Allowed
		}
	}
	resp, err := vaultgql.SetAppPluginPolicy(ctx, s.gqlClient(), input)
	if err != nil {
		return err
	}
	return firstMutationError(resp.SetAppPluginPolicy.Errors)
}

// firstMutationError converts a GraphQL mutation error list to a Go
// error (nil when empty).
func firstMutationError(errs []vaultgql.SetAppPluginPolicySetAppPluginPolicySetAppPluginPolicyMutationErrorsErrorType) error {
	for _, e := range errs {
		if len(e.Messages) > 0 {
			return &mutationError{message: e.Messages[0]}
		}
	}
	return nil
}

type mutationError struct{ message string }

func (m *mutationError) Error() string { return m.message }
