//go:build !windows

package autoupdate

import (
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// suppressStdout redirects stdout to /dev/null at the file descriptor level.
// Returns a function to restore stdout. This is needed because the go-selfupdate
// library writes directly to file descriptor 1, bypassing os.Stdout.
func suppressStdout() func() {
	// Save the original stdout file descriptor
	origStdout, err := syscall.Dup(syscall.Stdout)
	if err != nil {
		return func() {} // Can't suppress, return no-op
	}

	// Open /dev/null
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		syscall.Close(origStdout)
		return func() {}
	}

	// Redirect stdout to /dev/null
	// Use unix.Dup2 instead of syscall.Dup2 for cross-platform compatibility
	// (syscall.Dup2 is not available on linux/arm64)
	if err := unix.Dup2(int(devNull.Fd()), syscall.Stdout); err != nil {
		devNull.Close()
		syscall.Close(origStdout)
		return func() {}
	}

	devNull.Close()

	// Return function to restore stdout
	return func() {
		unix.Dup2(origStdout, syscall.Stdout)
		syscall.Close(origStdout)
	}
}
