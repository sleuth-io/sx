package vault

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/sleuth-io/sx/internal/manifest"
	vaultgql "github.com/sleuth-io/sx/internal/vault/graphql"
)

// Sleuth-vault extension support (the spec's P5): the server stores
// app-plugin assets through the normal asset pipeline and carries the
// org's extension policy; sx reads and writes it through GraphQL. The
// server enforces the org-admin gate and appends the policy audit event
// itself, so unlike the file vaults there is no client-side RBAC here.
// Shared extension storage (storage:shared, API 1.5.0) maps to the
// appPluginStorage query/mutation — the server enforces the size cap
// and id validation, so the client stays a thin pass-through.

// isAppPluginSchemaUnknownErr reports whether err is the server saying
// its SCHEMA has no app-plugin surface (a deployment predating P5) —
// as opposed to a transient failure that says nothing about capability.
// Graphene phrases both the unknown field and the rejected enum value
// as validation errors; the markers are pinned by tests so a reworded
// server error breaks loudly instead of silently reclassifying.
func isAppPluginSchemaUnknownErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, m := range []string{
		"Cannot query field", // unknown appPluginPolicy field
		"got invalid value",  // enum value rejected at variable coercion
		"cannot represent",   // enum coercion phrasing variant
	} {
		if strings.Contains(msg, m) {
			return true
		}
	}
	return false
}

// SupportsAppPlugins probes whether the connected server knows the
// app-plugin surface at all — deployed servers predating it reject both
// the asset type and the policy query. The policy read doubles as the
// capability check. definitive is false for transient failures, so
// callers don't durably cache an answer the server never gave.
func (s *SleuthVault) SupportsAppPlugins(ctx context.Context) (supported, definitive bool) {
	_, err := vaultgql.GetAppPluginPolicy(ctx, s.gqlClient())
	if err == nil {
		return true, true
	}
	if isAppPluginSchemaUnknownErr(err) {
		return false, true
	}
	return false, false
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

// AppPluginSharedLoad reads the extension's shared document ("" when
// none is stored) — the server twin of .sx/app-plugins/<id>.json.
func (s *SleuthVault) AppPluginSharedLoad(ctx context.Context, pluginID string) (string, error) {
	resp, err := vaultgql.GetAppPluginStorage(ctx, s.gqlClient(), pluginID)
	if err != nil {
		if isAppPluginSchemaUnknownErr(err) {
			return "", ErrSharedStorageUnsupported
		}
		return "", err
	}
	raw := resp.Vault.AppPluginStorage
	if raw == nil {
		return "", nil
	}
	// JSONString is a STRING scalar: the document arrives as a quoted
	// JSON string value, not inline JSON.
	var doc string
	if err := json.Unmarshal(*raw, &doc); err != nil {
		return "", err
	}
	return doc, nil
}

// AppPluginSharedSave replaces the extension's shared document; empty
// data deletes it. The server enforces the size cap and id validation.
func (s *SleuthVault) AppPluginSharedSave(ctx context.Context, pluginID, data string) error {
	input := vaultgql.SetAppPluginStorageInput{PluginId: pluginID}
	if data != "" {
		// Same contract the file vaults enforce locally: bounded valid
		// JSON, checked before anything leaves the machine.
		if len(data) > maxAppPluginSharedBytes {
			return fmt.Errorf("shared extension data exceeds %d bytes", maxAppPluginSharedBytes)
		}
		if !json.Valid([]byte(data)) {
			return errors.New("shared extension data must be valid JSON")
		}
		quoted, err := json.Marshal(data) // JSONString: document as a string value
		if err != nil {
			return err
		}
		raw := json.RawMessage(quoted)
		input.Data = &raw
	}
	resp, err := vaultgql.SetAppPluginStorage(ctx, s.gqlClient(), input)
	if err != nil {
		if isAppPluginSchemaUnknownErr(err) {
			return ErrSharedStorageUnsupported
		}
		return err
	}
	for _, e := range resp.SetAppPluginStorage.Errors {
		if len(e.Messages) > 0 {
			return &mutationError{message: e.Messages[0]}
		}
	}
	return nil
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
