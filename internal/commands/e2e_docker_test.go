package commands

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// DockerClientTest provides reusable infrastructure for testing AI client
// integrations via Docker. Tests using this helper are skipped unless
// SX_E2E=1 is set — they require Docker and are too heavy for CI.
type DockerClientTest struct {
	t        *testing.T
	TestDir  string
	VaultDir string
	SxBinary string

	// Resolved during Setup
	ClientHome       string // e.g. testDir/client-home
	FakeHome         string // e.g. testDir/fakehome
	SxConfigDir      string // host sx config
	ContainerSxConfig string // container sx config

	// Client-specific configuration (set before calling Setup)
	ClientName    string
	Image         string
	HomeInside    string // e.g. "/home/node"
	ConfigDirName string // e.g. ".openclaw"
	SkillsDirName string // e.g. "skills"

	// ComposeFunc generates docker-compose.yml content using resolved paths.
	ComposeFunc func(d *DockerClientTest) string
}

// skipUnlessE2E skips the test unless SX_E2E=1 is set.
func skipUnlessE2E(t *testing.T) {
	t.Helper()
	if os.Getenv("SX_E2E") != "1" {
		t.Skip("Skipping e2e Docker test (set SX_E2E=1 to run)")
	}
}

// NewDockerClientTest creates a new Docker-based e2e test harness.
func NewDockerClientTest(t *testing.T) *DockerClientTest {
	t.Helper()
	skipUnlessE2E(t)

	sxBinary, err := filepath.Abs("../../dist/sx")
	if err != nil {
		t.Fatalf("Failed to resolve sx binary path: %v", err)
	}
	if _, err := os.Stat(sxBinary); err != nil {
		t.Fatalf("sx binary not found at %s — run 'make build' first", sxBinary)
	}

	if err := exec.Command("docker", "info").Run(); err != nil {
		t.Fatalf("Docker is not running: %v", err)
	}

	testDir := t.TempDir()

	return &DockerClientTest{
		t:        t,
		TestDir:  testDir,
		VaultDir: filepath.Join(testDir, "vault"),
		SxBinary: sxBinary,
	}
}

// CreateVaultSkill packages a skill into the test vault.
func (d *DockerClientTest) CreateVaultSkill(name, version, description, content string) {
	d.t.Helper()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	metadata := fmt.Sprintf(`[asset]
name = "%s"
version = "%s"
type = "skill"
description = "%s"

[skill]
prompt-file = "SKILL.md"
`, name, version, description)

	skillMD := fmt.Sprintf(`---
name: %s
description: "%s"
user-invocable: true
---

%s
`, name, description, content)

	for fname, data := range map[string]string{
		"metadata.toml": metadata,
		"SKILL.md":      skillMD,
	} {
		w, err := zw.Create(fname)
		if err != nil {
			d.t.Fatalf("Failed to create zip entry %s: %v", fname, err)
		}
		if _, err := w.Write([]byte(data)); err != nil {
			d.t.Fatalf("Failed to write zip entry %s: %v", fname, err)
		}
	}
	if err := zw.Close(); err != nil {
		d.t.Fatalf("Failed to close zip: %v", err)
	}

	vaultAssetDir := filepath.Join(d.VaultDir, name, version)
	if err := os.MkdirAll(vaultAssetDir, 0755); err != nil {
		d.t.Fatalf("Failed to create vault dir: %v", err)
	}
	zipPath := filepath.Join(vaultAssetDir, fmt.Sprintf("%s-%s.zip", name, version))
	if err := os.WriteFile(zipPath, buf.Bytes(), 0644); err != nil {
		d.t.Fatalf("Failed to write zip: %v", err)
	}
	if err := os.WriteFile(filepath.Join(vaultAssetDir, "metadata.toml"), []byte(metadata), 0644); err != nil {
		d.t.Fatalf("Failed to write metadata.toml: %v", err)
	}

	listPath := filepath.Join(d.VaultDir, name, "list.txt")
	f, err := os.OpenFile(listPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		d.t.Fatalf("Failed to open list.txt: %v", err)
	}
	defer f.Close()
	fmt.Fprintln(f, version)
}

// Setup creates the sx profile, fake home, and Docker compose file.
// Must be called after setting ClientName, ConfigDirName, SkillsDirName, and ComposeFunc.
func (d *DockerClientTest) Setup() {
	d.t.Helper()

	if err := os.MkdirAll(d.VaultDir, 0755); err != nil {
		d.t.Fatalf("Failed to create vault dir: %v", err)
	}

	d.ClientHome = filepath.Join(d.TestDir, "client-home")
	d.FakeHome = filepath.Join(d.TestDir, "fakehome")
	d.SxConfigDir = filepath.Join(d.TestDir, "sx-config")
	d.ContainerSxConfig = filepath.Join(d.TestDir, "container-sx-config")

	configDir := filepath.Join(d.ClientHome, d.ConfigDirName)
	for _, dir := range []string{
		filepath.Join(configDir, d.SkillsDirName),
		d.FakeHome,
		d.SxConfigDir,
		d.ContainerSxConfig,
	} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			d.t.Fatalf("Failed to create dir %s: %v", dir, err)
		}
	}

	// Minimal client config
	if err := os.WriteFile(filepath.Join(configDir, d.ClientName+".json"), []byte("{}"), 0644); err != nil {
		d.t.Fatalf("Failed to write config: %v", err)
	}

	// Symlink so sx sees ~/.<client>
	if err := os.Symlink(configDir, filepath.Join(d.FakeHome, d.ConfigDirName)); err != nil {
		d.t.Fatalf("Failed to create symlink: %v", err)
	}

	// Host sx config
	hostConfig := fmt.Sprintf(`{
  "defaultProfile": "e2etest",
  "profiles": {
    "e2etest": {
      "type": "path",
      "repositoryUrl": "%s"
    }
  },
  "forceEnabledClients": ["%s"],
  "forceDisabledClients": []
}`, d.VaultDir, d.ClientName)
	if err := os.WriteFile(filepath.Join(d.SxConfigDir, "config.json"), []byte(hostConfig), 0644); err != nil {
		d.t.Fatalf("Failed to write sx config: %v", err)
	}

	// Container sx config (vault at /vault inside container)
	containerConfig := fmt.Sprintf(`{
  "defaultProfile": "e2etest",
  "profiles": {
    "e2etest": {
      "type": "path",
      "repositoryUrl": "/vault"
    }
  },
  "forceEnabledClients": ["%s"],
  "forceDisabledClients": []
}`, d.ClientName)
	if err := os.WriteFile(filepath.Join(d.ContainerSxConfig, "config.json"), []byte(containerConfig), 0644); err != nil {
		d.t.Fatalf("Failed to write container sx config: %v", err)
	}

	// Generate and write docker-compose.yml
	if d.ComposeFunc == nil {
		d.t.Fatal("ComposeFunc must be set before calling Setup()")
	}
	composeYAML := d.ComposeFunc(d)
	if err := os.WriteFile(filepath.Join(d.TestDir, "docker-compose.yml"), []byte(composeYAML), 0644); err != nil {
		d.t.Fatalf("Failed to write docker-compose.yml: %v", err)
	}
}

// RunSx runs the sx binary with the test profile from the host.
func (d *DockerClientTest) RunSx(args ...string) (string, error) {
	fullArgs := append([]string{"--profile=e2etest"}, args...)
	cmd := exec.Command(d.SxBinary, fullArgs...)
	cmd.Env = append(os.Environ(),
		"HOME="+d.FakeHome,
		"SX_CONFIG_DIR="+d.SxConfigDir,
	)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// AddSkill adds a vault skill to the sx lock file.
func (d *DockerClientTest) AddSkill(name, version string) {
	d.t.Helper()
	zipPath := filepath.Join(d.VaultDir, name, version, fmt.Sprintf("%s-%s.zip", name, version))
	out, err := d.RunSx("add", zipPath, "--yes", "--scope-global", "--no-install")
	if err != nil {
		d.t.Fatalf("Failed to add skill %s: %v\n%s", name, err, out)
	}
}

// InstallFromHost runs sx install from the host targeting the client.
func (d *DockerClientTest) InstallFromHost() string {
	d.t.Helper()
	out, err := d.RunSx("install", "--client="+d.ClientName)
	if err != nil {
		d.t.Fatalf("sx install failed: %v\n%s", err, out)
	}
	return out
}

// StartContainer pulls the image and starts the Docker container.
func (d *DockerClientTest) StartContainer() {
	d.t.Helper()
	composePath := filepath.Join(d.TestDir, "docker-compose.yml")

	d.t.Logf("Pulling image: %s", d.Image)
	if out, err := exec.Command("docker", "pull", d.Image).CombinedOutput(); err != nil {
		d.t.Fatalf("Failed to pull image: %v\n%s", err, string(out))
	}

	d.t.Log("Starting container...")
	if out, err := exec.Command("docker", "compose", "-f", composePath, "up", "-d").CombinedOutput(); err != nil {
		d.t.Fatalf("Failed to start container: %v\n%s", err, string(out))
	}

	d.t.Cleanup(func() {
		exec.Command("docker", "compose", "-f", composePath, "down", "--remove-orphans").CombinedOutput()
	})

	d.waitForHealthy(120 * time.Second)
}

func (d *DockerClientTest) waitForHealthy(timeout time.Duration) {
	d.t.Helper()
	composePath := filepath.Join(d.TestDir, "docker-compose.yml")
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		out, _ := exec.Command("docker", "compose", "-f", composePath, "ps", "--format", "json").CombinedOutput()
		if strings.Contains(string(out), `"healthy"`) {
			d.t.Log("Container is healthy")
			return
		}
		time.Sleep(3 * time.Second)
	}
	logs, _ := exec.Command("docker", "compose", "-f", composePath, "logs").CombinedOutput()
	d.t.Fatalf("Container failed to become healthy within %v\n%s", timeout, string(logs))
}

// RestartContainer restarts the container and waits for healthy.
func (d *DockerClientTest) RestartContainer() {
	d.t.Helper()
	composePath := filepath.Join(d.TestDir, "docker-compose.yml")
	if out, err := exec.Command("docker", "compose", "-f", composePath, "restart").CombinedOutput(); err != nil {
		d.t.Fatalf("Failed to restart: %v\n%s", err, string(out))
	}
	time.Sleep(5 * time.Second)
	d.waitForHealthy(90 * time.Second)
}

// ServiceName returns the compose service name (defaults to ClientName + "-gateway").
func (d *DockerClientTest) ServiceName() string {
	return d.ClientName + "-gateway"
}

// RunInContainer executes a command inside the running container.
func (d *DockerClientTest) RunInContainer(args ...string) (string, error) {
	composePath := filepath.Join(d.TestDir, "docker-compose.yml")
	cmdArgs := append([]string{"compose", "-f", composePath, "exec", "-T", d.ServiceName()}, args...)
	out, err := exec.Command("docker", cmdArgs...).CombinedOutput()
	return string(out), err
}

// SyncLockFile copies the lock file to the container's sx config directory.
func (d *DockerClientTest) SyncLockFile() {
	d.t.Helper()
	lockSrc := filepath.Join(d.VaultDir, "sx.lock")
	if _, err := os.Stat(lockSrc); err != nil {
		return
	}
	data, err := os.ReadFile(lockSrc)
	if err != nil {
		return
	}
	os.WriteFile(filepath.Join(d.ContainerSxConfig, "sx.lock"), data, 0644)
}

// InstallInContainer runs sx install inside the container.
func (d *DockerClientTest) InstallInContainer() (string, error) {
	d.SyncLockFile()
	return d.RunInContainer("sx", "install", "--client="+d.ClientName)
}

// SkillsDir returns the path to skills inside the client home on the host.
func (d *DockerClientTest) SkillsDir() string {
	return filepath.Join(d.ClientHome, d.ConfigDirName, d.SkillsDirName)
}

// SkillFileExists checks if a skill file exists on the host filesystem.
func (d *DockerClientTest) SkillFileExists(skillName, fileName string) bool {
	_, err := os.Stat(filepath.Join(d.SkillsDir(), skillName, fileName))
	return err == nil
}
