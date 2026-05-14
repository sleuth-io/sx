package commands

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/sx/internal/cache"
	"github.com/sleuth-io/sx/internal/ui"
	"github.com/sleuth-io/sx/internal/utils"
)

// executableFn is the function used to locate the running sx binary.
// Overridable by tests so they don't try to delete the real test binary.
var executableFn = os.Executable

// SelfUninstallOptions controls the self-uninstall flow.
type SelfUninstallOptions struct {
	Yes         bool
	DryRun      bool
	KeepAssets  bool
	Verbose     bool
	keepBinary  bool // test hook: skip the binary-removal step
	skipConfirm bool // test hook: skip the confirmation prompt
}

// NewSelfUninstallCommand creates the self-uninstall command.
func NewSelfUninstallCommand() *cobra.Command {
	opts := SelfUninstallOptions{}

	cmd := &cobra.Command{
		Use:   "self-uninstall",
		Short: "Completely remove sx, its config, cache, and all installed assets",
		Long: `Completely remove sx from your machine.

This is the inverse of the curl|bash installer. It will:
  1. Uninstall every asset sx has installed across all scopes and clients
     (skills, MCP servers, hooks, etc. — same as 'sx uninstall --all').
  2. Delete the sx config directory.
  3. Delete the sx cache directory.
  4. Delete the sx binary itself.

This action is irreversible. Use --dry-run to preview, or --keep-assets to
leave installed assets in place.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSelfUninstall(cmd, opts)
		},
	}

	cmd.Flags().BoolVar(&opts.Yes, "yes", false, "Skip confirmation prompt")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "Show what would be removed without making changes")
	cmd.Flags().BoolVar(&opts.KeepAssets, "keep-assets", false, "Do not uninstall assets — only remove sx itself")
	cmd.Flags().BoolVar(&opts.Verbose, "verbose", false, "Verbose output")

	return cmd
}

// selfUninstallPaths captures the on-disk locations sx owns.
type selfUninstallPaths struct {
	binary    string // empty if it could not be resolved
	binaryErr error
	configDir string
	configErr error
	cacheDir  string
	cacheErr  error
}

func resolveSelfUninstallPaths() selfUninstallPaths {
	p := selfUninstallPaths{}
	p.binary, p.binaryErr = executableFn()
	p.configDir, p.configErr = utils.GetConfigDir()
	p.cacheDir, p.cacheErr = cache.GetCacheDir()
	return p
}

func runSelfUninstall(cmd *cobra.Command, opts SelfUninstallOptions) error {
	styledOut := ui.NewOutput(cmd.OutOrStdout(), cmd.ErrOrStderr())

	paths := resolveSelfUninstallPaths()

	// Display the plan up front.
	styledOut.Header("This will completely remove sx from your machine.")
	styledOut.Newline()
	styledOut.Println("The following will be removed:")
	styledOut.Newline()
	if !opts.KeepAssets {
		styledOut.ListItem("•", "All installed assets (skills, MCP servers, hooks) across every scope and client")
	} else {
		styledOut.ListItem("•", "Installed assets: kept (--keep-assets)")
	}
	styledOut.ListItem("•", "Config:  "+displayPath(paths.configDir, paths.configErr))
	styledOut.ListItem("•", "Cache:   "+displayPath(paths.cacheDir, paths.cacheErr))
	styledOut.ListItem("•", "Binary:  "+displayPath(paths.binary, paths.binaryErr))
	styledOut.Newline()
	styledOut.Warning("This action is irreversible.")
	styledOut.Newline()

	// Dry run: stop here.
	if opts.DryRun {
		styledOut.Muted("No changes made (dry run).")
		return nil
	}

	// Confirm.
	if !opts.Yes && !opts.skipConfirm {
		if !confirmSelfUninstall(styledOut) {
			styledOut.Muted("Cancelled. Nothing was removed.")
			return nil
		}
	}

	// Step 1: uninstall all assets via the existing flow.
	if !opts.KeepAssets {
		styledOut.Newline()
		styledOut.Header("Uninstalling assets...")
		assetOpts := UninstallOptions{
			All:     true,
			Yes:     true,
			Verbose: opts.Verbose,
		}
		if err := runUninstall(cmd, nil, assetOpts); err != nil {
			styledOut.Warning(fmt.Sprintf("Asset cleanup reported: %v", err))
			styledOut.Muted("Continuing with config, cache, and binary removal.")
		}
	}

	// Step 2: remove config directory.
	styledOut.Newline()
	styledOut.Header("Removing config and cache...")
	if paths.configErr != nil {
		styledOut.Warning(fmt.Sprintf("Could not determine config dir: %v", paths.configErr))
	} else if err := removeDirIfExists(paths.configDir); err != nil {
		styledOut.Warning(fmt.Sprintf("Failed to remove config dir %s: %v", paths.configDir, err))
	} else {
		styledOut.SuccessItem("Removed " + paths.configDir)
	}

	// Step 3: remove cache directory.
	if paths.cacheErr != nil {
		styledOut.Warning(fmt.Sprintf("Could not determine cache dir: %v", paths.cacheErr))
	} else if err := removeDirIfExists(paths.cacheDir); err != nil {
		styledOut.Warning(fmt.Sprintf("Failed to remove cache dir %s: %v", paths.cacheDir, err))
	} else {
		styledOut.SuccessItem("Removed " + paths.cacheDir)
	}

	// Step 4: remove the binary itself.
	if !opts.keepBinary {
		styledOut.Newline()
		styledOut.Header("Removing binary...")
		removeBinary(paths, styledOut)
	}

	styledOut.Newline()
	styledOut.Success("sx has been removed.")
	styledOut.Muted("If you added the install directory to your shell's PATH, you can remove that line from your shell rc file.")
	return nil
}

// removeBinary deletes the running sx executable. On Unix this works while
// the process is still running because the kernel keeps the open inode alive
// until the process exits. On Windows the OS holds an exclusive lock, so we
// print a manual instruction instead.
func removeBinary(paths selfUninstallPaths, styledOut *ui.Output) {
	if paths.binaryErr != nil || paths.binary == "" {
		styledOut.Warning(fmt.Sprintf("Could not locate sx binary: %v", paths.binaryErr))
		return
	}

	if runtime.GOOS == "windows" {
		styledOut.Warning("Cannot self-delete on Windows. Please remove the binary manually: " + paths.binary)
		return
	}

	if err := os.Remove(paths.binary); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			styledOut.SuccessItem("Binary already gone: " + paths.binary)
			return
		}
		styledOut.Warning(fmt.Sprintf("Failed to remove binary %s: %v", paths.binary, err))
		styledOut.Muted("Please remove it manually.")
		return
	}
	styledOut.SuccessItem("Removed " + paths.binary)
}

// removeDirIfExists is RemoveAll that ignores a missing target.
func removeDirIfExists(path string) error {
	if path == "" {
		return nil
	}
	err := os.RemoveAll(path)
	if err != nil && errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// displayPath returns a printable representation of a resolved path, or a
// placeholder when resolution failed.
func displayPath(path string, err error) string {
	if err != nil {
		return fmt.Sprintf("<unresolved: %v>", err)
	}
	if path == "" {
		return "<not set>"
	}
	return path
}

// confirmSelfUninstall prompts the user; default is No.
func confirmSelfUninstall(styledOut *ui.Output) bool {
	reader := bufio.NewReader(os.Stdin)
	styledOut.Printf("Continue with self-uninstall? [y/N]: ")

	response, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	response = strings.ToLower(strings.TrimSpace(response))
	return response == "y" || response == "yes"
}
