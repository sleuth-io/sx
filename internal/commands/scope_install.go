package commands

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/sx/internal/lockfile"
)

// runInstallSetTarget points an existing asset at one or more scopes. It runs
// the exact same path as `sx add`'s flag mode: resolveScopeFromFlags resolves
// the (already-folded) scope flags, previews the diff, and—unless autoYes—
// confirms; updateLockFile then persists through the bulk installer. There is
// no install-specific scoping logic anymore (SD-10189).
func runInstallSetTarget(cmd *cobra.Command, assetName string, flags scopeFlags, autoYes bool) error {
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

	// Resolve, preview, and confirm exactly as `sx add` does. The "me" alias,
	// team/user/bot resolution, and append-vs-replace all live in the shared
	// path; this command only supplies the flags and the asset name.
	out := newOutputHelper(cmd)
	current, installed := resolveCurrentTargets(ctx, v, assetName)
	result, err := resolveScopeFromFlags(out, assetName, current, installed, flags, autoYes)
	if err != nil {
		return err
	}
	// No change to apply (nothing differed, or the user declined the prompt);
	// resolveScopeFromFlags already printed the reason.
	if result.Inherit {
		return nil
	}

	if err := updateLockFile(ctx, out, v, &lockfile.Asset{Name: assetName}, result); err != nil {
		return err
	}
	out.printf("✓ Updated scope for %s\n", assetName)
	return nil
}
