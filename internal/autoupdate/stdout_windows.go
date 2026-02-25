//go:build windows

package autoupdate

// suppressStdout returns a no-op on Windows.
// The go-selfupdate library output will still appear on Windows,
// but this is acceptable as Windows is a secondary platform.
func suppressStdout() func() {
	return func() {}
}
