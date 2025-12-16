package commands

import (
	"context"

	"github.com/sleuth-io/skills/internal/lockfile"
	"github.com/sleuth-io/skills/internal/vault"
	"github.com/sleuth-io/skills/internal/ui/components"
)

// updateLockFile updates the repository's lock file with the artifact using modern UI
func updateLockFile(ctx context.Context, out *outputHelper, repo vault.Vault, artifact *lockfile.Artifact) error {
	status := components.NewStatus(out.cmd.OutOrStdout())

	// For git repos, update the lock file and commit
	if gitRepo, ok := repo.(*vault.GitVault); ok {
		status.Start("Updating repository lock file")
		lockFilePath := gitRepo.GetLockFilePath()
		if err := lockfile.AddOrUpdateArtifact(lockFilePath, artifact); err != nil {
			status.Fail("Failed to update lock file")
			return err
		}

		if artifact.IsGlobal() {
			status.Done("Updated lock file (global installation)")
		} else {
			status.Done("Updated lock file with repository installation(s)")
		}

		status.Start("Committing and pushing to repository")
		if err := gitRepo.CommitAndPush(ctx, artifact); err != nil {
			status.Fail("Failed to push changes")
			return err
		}
		status.Done("Changes pushed to repository")
		return nil
	}

	// For path repos, update the lock file directly
	if pathRepo, ok := repo.(*vault.PathVault); ok {
		status.Start("Updating repository lock file")
		lockFilePath := pathRepo.GetLockFilePath()
		if err := lockfile.AddOrUpdateArtifact(lockFilePath, artifact); err != nil {
			status.Fail("Failed to update lock file")
			return err
		}

		if artifact.IsGlobal() {
			status.Done("Updated lock file (global installation)")
		} else {
			status.Done("Updated lock file with repository installation(s)")
		}
		return nil
	}

	return nil
}
