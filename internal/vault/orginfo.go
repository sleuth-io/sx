package vault

import (
	"context"

	vaultgql "github.com/sleuth-io/sx/internal/vault/graphql"
)

// OrgInfo reports the organization behind a sleuth vault: its display name
// and icon URL ("" when the org has none). Used by the desktop app to show
// the org's icon for skills.new libraries.
func (s *SleuthVault) OrgInfo(ctx context.Context) (name, iconURL string, err error) {
	resp, err := vaultgql.OrgInfo(ctx, s.gqlClient())
	if err != nil {
		return "", "", err
	}
	icon := ""
	if resp.Organization.IconUrl != nil {
		icon = *resp.Organization.IconUrl
	}
	return resp.Organization.Name, icon, nil
}
