package commands

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/sx/internal/config"
	vaultpkg "github.com/sleuth-io/sx/internal/vault"
)

// NewCollectionCommand returns the `sx collection` command group.
// Collections are created and managed in the desktop app; the CLI surface is
// read-only for now.
func NewCollectionCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "collection",
		Short: "Work with asset collections",
		Long:  "Collections are named groupings of assets, managed in the sx desktop app.",
	}
	cmd.AddCommand(newCollectionListCommand())
	return cmd
}

func newCollectionListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the vault's collections",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			out := newOutputHelper(cmd)

			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("failed to load configuration: %w\nRun 'sx init' to configure", err)
			}
			vault, err := vaultpkg.NewFromConfig(cfg)
			if err != nil {
				return fmt.Errorf("failed to create vault: %w", err)
			}
			store, ok := vault.(vaultpkg.CollectionStore)
			if !ok {
				return errors.New("this vault type does not support collections yet")
			}
			collections, err := store.ListCollections(ctx)
			if err != nil {
				return err
			}
			if len(collections) == 0 {
				out.println("No collections yet. Create them in the sx desktop app.")
				return nil
			}
			for _, c := range collections {
				out.printf("%s (%d assets)\n", c.Name, len(c.Assets))
				if c.Description != "" {
					out.printf("  %s\n", c.Description)
				}
				if len(c.Assets) > 0 {
					out.printf("  %s\n", strings.Join(c.Assets, ", "))
				}
			}
			return nil
		},
	}
}
