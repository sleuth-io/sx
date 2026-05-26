package sxvault

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestGitPutAgentWritesSXVaultFormat(t *testing.T) {
	ctx := context.Background()
	remote, client := newGitVaultClient(t)
	if _, err := client.PutAgent(ctx, AgentSpec{
		BotName:     "reviewer",
		AssetName:   "reviewer",
		Version:     "1.0.0",
		DisplayName: "Reviewer",
		Description: "Reviews pull requests.",
		Prompt:      "You are Reviewer.",
	}); err != nil {
		t.Fatal(err)
	}

	clone := cloneRemote(t, remote)
	assertFileContains(t, filepath.Join(clone, "assets", "reviewer", "1.0.0", "AGENT.md"), "You are Reviewer.")
	assertFileContains(t, filepath.Join(clone, "assets", "reviewer", "1.0.0", "metadata.toml"), `type = "agent"`)
	manifest := readFile(t, filepath.Join(clone, "sx.toml"))
	for _, want := range []string{`name = "reviewer"`, `kind = "bot"`, `bot = "reviewer"`} {
		if !strings.Contains(manifest, want) {
			t.Fatalf("sx.toml missing %q:\n%s", want, manifest)
		}
	}
}

func TestEnsureBotExistingAndDescriptionUpdate(t *testing.T) {
	ctx := context.Background()
	remote, client := newGitVaultClient(t)

	token, err := client.EnsureBot(ctx, Bot{Name: "ci", Description: "first"})
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		t.Fatalf("git vault EnsureBot token = %q, want empty", token)
	}
	token, err = client.EnsureBot(ctx, Bot{Name: "ci", Description: "updated"})
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		t.Fatalf("existing bot token = %q, want empty", token)
	}

	clone := cloneRemote(t, remote)
	manifest := readFile(t, filepath.Join(clone, "sx.toml"))
	for _, want := range []string{`name = "ci"`, `description = "updated"`} {
		if !strings.Contains(manifest, want) {
			t.Fatalf("sx.toml missing %q:\n%s", want, manifest)
		}
	}
}

func TestPutAgentSameVersionIsIdempotent(t *testing.T) {
	ctx := context.Background()
	remote, client := newGitVaultClient(t)
	spec := AgentSpec{
		BotName:     "reviewer",
		AssetName:   "reviewer",
		Version:     "1.0.0",
		DisplayName: "Reviewer",
		Description: "Reviews pull requests.",
		Prompt:      "You are Reviewer.",
	}
	if _, err := client.PutAgent(ctx, spec); err != nil {
		t.Fatal(err)
	}
	if _, err := client.PutAgent(ctx, spec); err != nil {
		t.Fatal(err)
	}

	clone := cloneRemote(t, remote)
	list := readFile(t, filepath.Join(clone, "assets", "reviewer", "list.txt"))
	if count := strings.Count(list, "1.0.0"); count != 1 {
		t.Fatalf("version list contains 1.0.0 %d times:\n%s", count, list)
	}
	assertFileContains(t, filepath.Join(clone, "assets", "reviewer", "1.0.0", "AGENT.md"), "You are Reviewer.")
}

func TestPutSkillZipWithAndWithoutBotInstall(t *testing.T) {
	ctx := context.Background()
	remote, client := newGitVaultClient(t)

	if err := client.PutSkillZip(ctx, SkillZipSpec{
		Name:        "lint-helper",
		Version:     "1.0.0",
		Description: "Helps with lint fixes.",
		ZipData:     skillZip(t, "Lint carefully."),
	}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := client.EnsureBot(ctx, Bot{Name: "reviewer", Description: "Reviews pull requests."}); err != nil {
		t.Fatal(err)
	}
	if err := client.PutSkillZip(ctx, SkillZipSpec{
		Name:        "test-helper",
		Version:     "1.0.0",
		Description: "Helps with test fixes.",
		ZipData:     skillZip(t, "Test carefully."),
	}, "reviewer"); err != nil {
		t.Fatal(err)
	}

	clone := cloneRemote(t, remote)
	assertFileContains(t, filepath.Join(clone, "assets", "lint-helper", "1.0.0", "SKILL.md"), "Lint carefully.")
	assertFileContains(t, filepath.Join(clone, "assets", "test-helper", "1.0.0", "SKILL.md"), "Test carefully.")
	manifest := readFile(t, filepath.Join(clone, "sx.toml"))
	if strings.Contains(manifest, `bot = "lint-helper"`) {
		t.Fatalf("skill without botName should not be installed to a bot:\n%s", manifest)
	}
	for _, want := range []string{`name = "test-helper"`, `kind = "bot"`, `bot = "reviewer"`} {
		if !strings.Contains(manifest, want) {
			t.Fatalf("sx.toml missing %q:\n%s", want, manifest)
		}
	}
}

func TestListAssetsWithOptionsHonorsLimit(t *testing.T) {
	ctx := context.Background()
	_, client := newGitVaultClient(t)

	for _, name := range []string{"lint-helper", "test-helper"} {
		if err := client.PutSkillZip(ctx, SkillZipSpec{
			Name:    name,
			Version: "1.0.0",
			ZipData: skillZip(t, name),
		}, ""); err != nil {
			t.Fatal(err)
		}
	}

	allSkills, err := client.ListAssets(ctx, "skill")
	if err != nil {
		t.Fatal(err)
	}
	if len(allSkills) != 2 {
		t.Fatalf("ListAssets returned %d skills, want 2: %+v", len(allSkills), allSkills)
	}
	limited, err := client.ListAssetsWithOptions(ctx, ListOptions{Type: "skill", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(limited) != 1 {
		t.Fatalf("ListAssetsWithOptions limit returned %d skills, want 1: %+v", len(limited), limited)
	}
}

func TestPutAgentValidationErrors(t *testing.T) {
	ctx := context.Background()
	_, client := newGitVaultClient(t)
	for _, spec := range []AgentSpec{
		{AssetName: "agent", Version: "1.0.0"},
		{BotName: "agent", Version: "1.0.0"},
		{BotName: "agent", AssetName: "agent"},
	} {
		if _, err := client.PutAgent(ctx, spec); err == nil {
			t.Fatalf("PutAgent(%+v) succeeded, want validation error", spec)
		}
	}
}

func TestOpenSkillsNewValidation(t *testing.T) {
	if _, err := OpenSkillsNew("", ""); err == nil {
		t.Fatal("OpenSkillsNew with empty token succeeded, want error")
	}
	if _, err := OpenSkillsNew("://bad", "token"); err == nil {
		t.Fatal("OpenSkillsNew with invalid URL succeeded, want error")
	}
	if _, err := OpenSkillsNew("https://app.skills.new", "token"); err != nil {
		t.Fatalf("OpenSkillsNew valid input: %v", err)
	}
	client, err := OpenSkillsNewWithOptions("https://app.skills.new", SkillsNewOptions{
		AuthToken: "token",
		Actor:     Actor{Name: "Admin", Email: "admin@example.com"},
	})
	if err != nil {
		t.Fatalf("OpenSkillsNewWithOptions valid input: %v", err)
	}
	if client.actor.Email != "admin@example.com" {
		t.Fatalf("OpenSkillsNewWithOptions actor email = %q", client.actor.Email)
	}
}

func TestOpenGitAuthTokenWithSSHRemoteDoesNotRequireHTTPSHost(t *testing.T) {
	client, err := OpenGit("git@gitlab.com:org/repo.git", GitOptions{AuthToken: "token"})
	if err != nil {
		t.Fatalf("OpenGit ssh remote with token: %v", err)
	}
	if hasGitBasicAuthEnv(client.gitExtraEnv) {
		t.Fatalf("SSH remote configured HTTPS basic auth env: %v", client.gitExtraEnv)
	}
	if _, err := OpenGit("https:///org/repo.git", GitOptions{AuthToken: "token"}); err == nil {
		t.Fatal("OpenGit malformed HTTPS remote with token succeeded, want error")
	}
	httpClient, err := OpenGit("http://git.example.test/org/repo.git", GitOptions{AuthToken: "token"})
	if err != nil {
		t.Fatalf("OpenGit http remote with token: %v", err)
	}
	if !strings.Contains(strings.Join(httpClient.gitExtraEnv, "\n"), "http.http://git.example.test/.extraheader") {
		t.Fatalf("HTTP remote did not configure HTTP basic auth env: %v", httpClient.gitExtraEnv)
	}
}

func newGitVaultClient(t *testing.T) (string, *Client) {
	t.Helper()
	t.Setenv("SX_CACHE_DIR", t.TempDir())
	remote := filepath.Join(t.TempDir(), "vault.git")
	runGit(t, "", "init", "--bare", remote)
	client, err := OpenGit(remote, GitOptions{Actor: Actor{Name: "Test Admin", Email: "test@example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	return remote, client
}

func cloneRemote(t *testing.T, remote string) string {
	t.Helper()
	clone := filepath.Join(t.TempDir(), "clone")
	runGit(t, "", "clone", "--branch", remoteBranch(t, remote), remote, clone)
	return clone
}

func remoteBranch(t *testing.T, remote string) string {
	t.Helper()
	out := runGitOutput(t, "", "--git-dir", remote, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	branches := strings.Fields(out)
	for _, branch := range []string{"main", "master"} {
		if slices.Contains(branches, branch) {
			return branch
		}
	}
	if len(branches) == 0 {
		t.Fatalf("remote %s has no branches", remote)
	}
	return branches[0]
}

func skillZip(t *testing.T, prompt string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(prompt)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func hasGitBasicAuthEnv(env []string) bool {
	for _, v := range env {
		if strings.Contains(v, "extraheader") || strings.HasPrefix(v, "GIT_CONFIG_COUNT=") {
			return true
		}
	}
	return false
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}

func runGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return string(out)
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		root := filepath.Dir(filepath.Dir(filepath.Dir(path)))
		entries := []string{}
		_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err == nil {
				entries = append(entries, p)
			}
			return nil
		})
		t.Fatalf("%v\nentries under %s:\n%s", err, root, strings.Join(entries, "\n"))
	}
	return string(data)
}

func assertFileContains(t *testing.T, path, want string) {
	t.Helper()
	got := readFile(t, path)
	if !strings.Contains(got, want) {
		t.Fatalf("%s missing %q:\n%s", path, want, got)
	}
}
