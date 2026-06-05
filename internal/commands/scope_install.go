package commands

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/sx/internal/ui/components"
	vaultpkg "github.com/sleuth-io/sx/internal/vault"
)

// installTargetFlags captures the scope flags that can put `sx install` into
// "set installation target" mode.
type installTargetFlags struct {
	org  bool
	repo string
	path string
	team string
	user string
	bot  string
}

func (f installTargetFlags) active() bool {
	return f.org || f.repo != "" || f.path != "" || f.team != "" || f.user != "" || f.bot != ""
}

func (f installTargetFlags) count() int {
	n := 0
	if f.org {
		n++
	}
	if f.repo != "" {
		n++
	}
	if f.path != "" {
		n++
	}
	if f.team != "" {
		n++
	}
	if f.user != "" {
		n++
	}
	if f.bot != "" {
		n++
	}
	return n
}

// errInstallExclusiveFlags is returned when more than one of --org,
// --repo, --path, --team, --user, --bot is set.
var errInstallExclusiveFlags = errors.New("exactly one of --org, --repo, --path, --team, --user, --bot may be set")

// runInstallSetTarget translates one active install flag into a call to
// Vault.SetAssetInstallation. Exactly one targeting flag must be set.
// For --team targets, the caller must be an admin of that team (mirrors
// skills.new's server-side permission check).
func runInstallSetTarget(cmd *cobra.Command, assetName string, f installTargetFlags) error {
	if f.count() != 1 {
		return errInstallExclusiveFlags
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	v, err := loadVault()
	if err != nil {
		return err
	}

	// Verify the asset exists in the chosen vault before mutating. In
	// multi-active setups the asset the user wants to retarget might be
	// owned by a non-default profile's vault, and writing to the default
	// vault would either fail mid-flight or (worse) succeed against an
	// unrelated row with the same name. Catch this up front and, when
	// possible, name the profile that actually owns the asset.
	if _, err := v.GetAssetDetails(ctx, assetName); err != nil {
		if owner := findAssetOwningProfile(ctx, assetName); owner != "" {
			return fmt.Errorf("asset %q not found in the default profile's vault: %w (asset lives in profile %q — retry with --profile %s)", assetName, err, owner, owner)
		}
		return fmt.Errorf("asset %q not found in the default profile's vault: %w (if it lives in another profile, retry with --profile <name>)", assetName, err)
	}

	target, err := buildInstallTarget(f)
	if err != nil {
		return err
	}

	if target.Kind == vaultpkg.InstallKindTeam {
		if err := requireTeamAdmin(ctx, v, target.Team); err != nil {
			return err
		}
	}

	status := components.NewStatus(cmd.OutOrStdout())
	status.Start("Setting installation target for " + assetName)
	if err := v.SetAssetInstallation(ctx, assetName, target); err != nil {
		status.Fail("Failed to set installation target")
		return err
	}
	status.Done("Updated installation target for " + assetName)
	return nil
}

func buildInstallTarget(f installTargetFlags) (vaultpkg.InstallTarget, error) {
	switch {
	case f.org:
		return vaultpkg.InstallTarget{Kind: vaultpkg.InstallKindOrg}, nil
	case f.repo != "":
		return vaultpkg.InstallTarget{Kind: vaultpkg.InstallKindRepo, Repo: f.repo}, nil
	case f.path != "":
		repo, paths := parseRepoSpec(f.path)
		if repo == "" || len(paths) == 0 {
			return vaultpkg.InstallTarget{}, errors.New("--path must be in the form repo_url#path1,path2")
		}
		return vaultpkg.InstallTarget{Kind: vaultpkg.InstallKindPath, Repo: repo, Paths: paths}, nil
	case f.team != "":
		return vaultpkg.InstallTarget{Kind: vaultpkg.InstallKindTeam, Team: f.team}, nil
	case f.user != "":
		return vaultpkg.InstallTarget{Kind: vaultpkg.InstallKindUser, User: f.user}, nil
	case f.bot != "":
		return vaultpkg.InstallTarget{Kind: vaultpkg.InstallKindBot, Bot: f.bot}, nil
	}
	return vaultpkg.InstallTarget{}, errors.New("no installation target specified")
}
