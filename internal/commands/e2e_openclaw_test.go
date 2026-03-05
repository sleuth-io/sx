package commands

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// TestE2EOpenClawSkillDiscovery verifies that OpenClaw discovers sx-installed skills.
// Requires: Docker, SX_E2E=1, API key in /home/mrdon/dev/pulse/.env
func TestE2EOpenClawSkillDiscovery(t *testing.T) {
	d := newOpenClawDockerTest(t)

	d.CreateVaultSkill("hello-world", "1.0.0",
		"A simple greeting skill",
		"# Hello World\n\nWhen invoked, respond with a friendly greeting.")

	d.Setup()
	d.AddSkill("hello-world", "1.0.0")
	d.InstallFromHost()

	if !d.SkillFileExists("hello-world", "SKILL.md") {
		t.Fatal("SKILL.md not found on host after install")
	}

	d.StartContainer()
	onboardOpenClaw(t, d)
	d.RestartContainer()

	out := openclawSkillsList(t, d)
	if !strings.Contains(out, "hello-world") {
		t.Errorf("hello-world not found in skills list:\n%s", out)
	}

	info, err := openclawSkillInfo(d, "hello-world")
	if err != nil {
		t.Fatalf("skills info failed: %v\n%s", err, info)
	}
	if !strings.Contains(info, "Ready") {
		t.Error("hello-world should be Ready")
	}
	if !strings.Contains(info, "openclaw-managed") {
		t.Error("hello-world source should be openclaw-managed")
	}
}

// TestE2EOpenClawSecondSkillInstall verifies adding a second skill via sx install in-container.
func TestE2EOpenClawSecondSkillInstall(t *testing.T) {
	d := newOpenClawDockerTest(t)

	d.CreateVaultSkill("hello-world", "1.0.0",
		"A greeting skill", "# Hello World")

	d.Setup()
	d.AddSkill("hello-world", "1.0.0")
	d.InstallFromHost()
	d.StartContainer()
	onboardOpenClaw(t, d)
	d.RestartContainer()

	d.CreateVaultSkill("farewell-world", "1.0.0",
		"A farewell skill", "# Farewell World")
	d.AddSkill("farewell-world", "1.0.0")

	out := openclawSkillsList(t, d)
	if strings.Contains(out, "farewell-world") {
		t.Error("farewell-world should NOT be visible before in-container install")
	}

	installOut, err := d.InstallInContainer()
	if err != nil {
		t.Fatalf("in-container install failed: %v\n%s", err, installOut)
	}

	out = openclawSkillsList(t, d)
	if !strings.Contains(out, "farewell-world") {
		t.Errorf("farewell-world not found after in-container install:\n%s", out)
	}
	if !strings.Contains(out, "hello-world") {
		t.Error("hello-world should still be present")
	}
}

// TestE2EOpenClawCronAutoUpdate verifies cron-based auto-update installs new skills.
func TestE2EOpenClawCronAutoUpdate(t *testing.T) {
	d := newOpenClawDockerTest(t)

	d.CreateVaultSkill("hello-world", "1.0.0",
		"A greeting skill", "# Hello World")

	d.Setup()
	d.AddSkill("hello-world", "1.0.0")
	d.InstallFromHost()
	d.StartContainer()
	onboardOpenClaw(t, d)
	d.RestartContainer()

	d.CreateVaultSkill("cron-test", "1.0.0",
		"Cron update test", "# Cron Test")
	d.AddSkill("cron-test", "1.0.0")
	d.SyncLockFile()

	d.RunInContainer("bash", "-c",
		`nohup bash -c "while true; do sleep 60; sx install --client=openclaw 2>/dev/null; done" &>/tmp/sx-cron.log &`)

	out := openclawSkillsList(t, d)
	if strings.Contains(out, "cron-test") {
		t.Error("cron-test should NOT be visible before cron fires")
	}

	t.Log("Waiting 70s for cron to fire...")
	time.Sleep(70 * time.Second)

	out = openclawSkillsList(t, d)
	if !strings.Contains(out, "cron-test") {
		t.Errorf("cron-test not auto-installed by cron:\n%s", out)
	}
}

// --- OpenClaw-specific helpers ---

func newOpenClawDockerTest(t *testing.T) *DockerClientTest {
	t.Helper()
	d := NewDockerClientTest(t)

	d.ClientName = "openclaw"
	d.Image = "ghcr.io/openclaw/openclaw:latest"
	d.HomeInside = "/home/node"
	d.ConfigDirName = ".openclaw"
	d.SkillsDirName = "skills"

	apiKey := loadAPIKey(t)

	d.ComposeFunc = func(d *DockerClientTest) string {
		return fmt.Sprintf(`services:
  openclaw-gateway:
    image: %s
    command: ["node", "openclaw.mjs", "gateway", "--allow-unconfigured"]
    working_dir: /app
    volumes:
      - %s/.openclaw:/home/node/.openclaw
      - %s:/usr/local/bin/sx:ro
      - %s:/vault:ro
      - %s:/home/node/.config/sx:ro
    environment:
      - ANTHROPIC_API_KEY=%s
      - HOME=/home/node
    ports:
      - "18789:18789"
    healthcheck:
      test: ["CMD", "node", "-e", "fetch('http://127.0.0.1:18789/healthz').then(r=>process.exit(r.ok?0:1)).catch(()=>process.exit(1))"]
      interval: 5s
      timeout: 5s
      start_period: 15s
      retries: 20
`, d.Image, d.ClientHome, d.SxBinary, d.VaultDir, d.ContainerSxConfig, apiKey)
	}

	return d
}

func loadAPIKey(t *testing.T) string {
	t.Helper()
	envFile := "/home/mrdon/dev/pulse/.env"
	data, err := os.ReadFile(envFile)
	if err != nil {
		t.Skipf("API key file not found: %s", envFile)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "SLEUTH_CLAUDE_API_KEY=") {
			return strings.TrimPrefix(line, "SLEUTH_CLAUDE_API_KEY=")
		}
	}
	t.Skip("SLEUTH_CLAUDE_API_KEY not found in env file")
	return ""
}

func onboardOpenClaw(t *testing.T, d *DockerClientTest) {
	t.Helper()
	apiKey := loadAPIKey(t)
	out, err := d.RunInContainer("node", "/app/openclaw.mjs",
		"onboard", "--non-interactive",
		"--accept-risk",
		"--anthropic-api-key", apiKey,
		"--mode", "local",
		"--skip-channels", "--skip-daemon", "--skip-skills", "--skip-ui", "--skip-health")
	if err != nil {
		t.Logf("Onboarding warning: %v\n%s", err, out)
	}
}

func openclawSkillsList(t *testing.T, d *DockerClientTest) string {
	t.Helper()
	out, err := d.RunInContainer("node", "/app/openclaw.mjs", "skills", "list", "--json")
	if err != nil {
		t.Fatalf("skills list failed: %v\n%s", err, out)
	}
	return out
}

func openclawSkillInfo(d *DockerClientTest, name string) (string, error) {
	return d.RunInContainer("node", "/app/openclaw.mjs", "skills", "info", name)
}
