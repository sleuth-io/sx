package git

import (
	"context"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Availability reports whether a usable git binary exists on this machine.
// Reason is a short, user-facing sentence when git is unusable.
type Availability struct {
	Available bool
	Version   string
	Reason    string
}

// CheckAvailability probes for a working git without ever triggering
// side effects. The macOS trap: on a Mac without the Xcode Command Line
// Tools, /usr/bin/git is a shim whose mere execution (even `git
// --version`) pops Apple's GUI installer dialog — so on darwin the
// developer-tools presence is checked first via `xcode-select -p`, and
// the shim is never run when the tools are missing.
func CheckAvailability(ctx context.Context) Availability {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return Availability{Reason: "Git isn't installed on this computer."}
	}

	if runtime.GOOS == "darwin" && gitPath == "/usr/bin/git" {
		// xcode-select exits non-zero when no developer directory is
		// configured, i.e. the Command Line Tools are not installed.
		probe, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		if err := exec.CommandContext(probe, "xcode-select", "-p").Run(); err != nil {
			return Availability{Reason: "Git needs Apple's command-line developer tools, which aren't installed."}
		}
	}

	probe, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(probe, gitPath, "--version").Output()
	if err != nil {
		return Availability{Reason: "Git is installed but isn't working (git --version failed)."}
	}
	version := strings.TrimSpace(string(out))
	version = strings.TrimPrefix(version, "git version ")
	return Availability{Available: true, Version: version}
}
