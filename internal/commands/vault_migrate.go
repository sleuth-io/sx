package commands

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/sx/internal/config"
	vaultpkg "github.com/sleuth-io/sx/internal/vault"
)

// storageMigrator is implemented by file-backed vaults (git, path) that
// support the v1 → v2 storage-format migration. The Sleuth vault stores
// assets server-side and has no client-visible layout, so it does not
// implement this.
type storageMigrator interface {
	MigrateStorage(ctx context.Context) (*vaultpkg.MigrationResult, error)
	PlanStorageMigration(ctx context.Context) (*vaultpkg.MigrationPlan, error)
}

func newVaultMigrateCommand() *cobra.Command {
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Migrate the vault to the current storage format",
		Long: `Migrate a v1 vault to storage format v2 in place.

Format v2 keeps the latest version of every asset directly at assets/{name}
(usable in place by editors, agents, and tools) and moves the immutable
version history to .sx/versions. Migration normally happens automatically on
the first write by an up-to-date sx; this command runs it explicitly.

After migration, older sx versions cannot use the vault at all — reads,
installs, and writes fail with an unsupported schema version until every
teammate upgrades. Coordinate the migration with your team.

Use --dry-run to preview what would move without changing anything.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			return runVaultMigrate(ctx, cmd, dryRun)
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview the migration without changing the vault")
	return cmd
}

func runVaultMigrate(ctx context.Context, cmd *cobra.Command, dryRun bool) error {
	out := newOutputHelper(cmd)

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w\nRun 'sx init' to configure", err)
	}
	vault, err := vaultpkg.NewFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("failed to create vault: %w", err)
	}

	migrator, ok := vault.(storageMigrator)
	if !ok {
		return errors.New("this vault type stores assets server-side and needs no storage migration")
	}

	plan, err := migrator.PlanStorageMigration(ctx)
	if err != nil {
		if errors.Is(err, vaultpkg.ErrStorageUpToDate) {
			out.println("Vault is already on the current storage format.")
			return nil
		}
		return err
	}

	out.printf("Vault storage format: v%d → v%d\n", plan.FromVersion, plan.ToVersion)
	if len(plan.Assets) == 0 {
		out.println("No stored assets to move (manifest-only migration).")
	} else {
		out.printf("%d asset(s) will move to .sx/versions with a browsable copy of the latest version left at assets/{name}:\n", len(plan.Assets))
		for _, name := range plan.Assets {
			out.printf("  - %s\n", name)
		}
	}

	if dryRun {
		out.println()
		out.println("Dry run — nothing changed. Re-run without --dry-run to migrate.")
		return nil
	}

	result, err := migrator.MigrateStorage(ctx)
	if err != nil {
		if errors.Is(err, vaultpkg.ErrStorageUpToDate) {
			out.println("Vault was already migrated (another client got there first).")
			return nil
		}
		return fmt.Errorf("migration failed: %w", err)
	}
	out.println()
	out.printf("✓ Migrated vault to storage format v2 (%d assets)\n", result.Assets)
	out.println("Note: older sx versions can no longer modify this vault — make sure your team upgrades.")
	return nil
}
