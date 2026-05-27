package sxvault

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sleuth-io/sx/internal/git"
)

func TestGitPutAgentWritesSXVaultFormat(t *testing.T) {
	ctx := context.Background()
	remote, client := newGitVaultClient(t)
	if _, err := client.PutAgent(ctx, AgentSpec{
		BotName:        "reviewer",
		AssetName:      "reviewer",
		Version:        "1.0.0",
		Description:    "Reviews pull requests.",
		BotDescription: "Reviewer bot.",
		Prompt:         "You are Reviewer.",
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
		BotName:        "reviewer",
		AssetName:      "reviewer",
		Version:        "1.0.0",
		Description:    "Reviews pull requests.",
		BotDescription: "Reviewer bot.",
		Prompt:         "You are Reviewer.",
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

func TestPutSkillZipSameVersionIsIdempotent(t *testing.T) {
	ctx := context.Background()
	remote, client := newGitVaultClient(t)
	spec := SkillZipSpec{
		Name:        "lint-helper",
		Version:     "1.0.0",
		Description: "Helps with lint fixes.",
		ZipData:     skillZip(t, "Lint carefully."),
	}
	if err := client.PutSkillZip(ctx, spec); err != nil {
		t.Fatal(err)
	}
	if err := client.PutSkillZip(ctx, spec); err != nil {
		t.Fatal(err)
	}

	clone := cloneRemote(t, remote)
	list := readFile(t, filepath.Join(clone, "assets", "lint-helper", "list.txt"))
	if count := strings.Count(list, "1.0.0"); count != 1 {
		t.Fatalf("version list contains 1.0.0 %d times:\n%s", count, list)
	}
	assertFileContains(t, filepath.Join(clone, "assets", "lint-helper", "1.0.0", "SKILL.md"), "Lint carefully.")
}

func TestPutSkillZipDescriptionPrecedence(t *testing.T) {
	ctx := context.Background()
	remote, client := newGitVaultClient(t)

	// Empty SkillZipSpec.Description preserves the description embedded in
	// the zip's metadata.toml.
	preservedZip := skillZipWithMetadata(t, "Lint carefully.", "Embedded description.")
	if err := client.PutSkillZip(ctx, SkillZipSpec{
		Name:    "lint-helper",
		Version: "1.0.0",
		ZipData: preservedZip,
	}); err != nil {
		t.Fatal(err)
	}

	// Non-empty SkillZipSpec.Description overrides the embedded description.
	overrideZip := skillZipWithMetadata(t, "Test carefully.", "Embedded description.")
	if err := client.PutSkillZip(ctx, SkillZipSpec{
		Name:        "test-helper",
		Version:     "1.0.0",
		Description: "Spec description wins.",
		ZipData:     overrideZip,
	}); err != nil {
		t.Fatal(err)
	}

	clone := cloneRemote(t, remote)
	preserved := readFile(t, filepath.Join(clone, "assets", "lint-helper", "1.0.0", "metadata.toml"))
	if !strings.Contains(preserved, `description = "Embedded description."`) {
		t.Fatalf("empty spec description did not preserve embedded description:\n%s", preserved)
	}
	overridden := readFile(t, filepath.Join(clone, "assets", "test-helper", "1.0.0", "metadata.toml"))
	if !strings.Contains(overridden, `description = "Spec description wins."`) {
		t.Fatalf("non-empty spec description did not override embedded description:\n%s", overridden)
	}
	if strings.Contains(overridden, `description = "Embedded description."`) {
		t.Fatalf("override left embedded description in metadata.toml:\n%s", overridden)
	}
}

func TestPutSkillZipWithAndWithoutBotInstall(t *testing.T) {
	ctx := context.Background()
	remote, client := newGitVaultClient(t)

	if err := client.PutSkillZip(ctx, SkillZipSpec{
		Name:        "lint-helper",
		Version:     "1.0.0",
		Description: "Helps with lint fixes.",
		ZipData:     skillZip(t, "Lint carefully."),
	}); err != nil {
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
		BotName:     "reviewer",
	}); err != nil {
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

func TestListAssetsWithOptionsHonorsLimitAndSearch(t *testing.T) {
	ctx := context.Background()
	_, client := newGitVaultClient(t)

	for _, name := range []string{"lint-helper", "test-helper"} {
		if err := client.PutSkillZip(ctx, SkillZipSpec{
			Name:    name,
			Version: "1.0.0",
			ZipData: skillZip(t, name),
		}); err != nil {
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
	filtered, err := client.ListAssetsWithOptions(ctx, ListOptions{Type: "skill", Search: "lint"})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 1 || filtered[0].Name != "lint-helper" {
		t.Fatalf("ListAssetsWithOptions search returned %+v, want lint-helper only", filtered)
	}
}

func TestPutAgentValidationErrors(t *testing.T) {
	ctx := context.Background()
	_, client := newGitVaultClient(t)
	for _, spec := range []AgentSpec{
		{AssetName: "agent", Version: "1.0.0", Prompt: "p"},
		{BotName: "agent", Version: "1.0.0", Prompt: "p"},
		{BotName: "agent", AssetName: "agent", Prompt: "p"},
		// Missing prompt must also fail — an agent with no instructions
		// is silently broken if the publish succeeds.
		{BotName: "agent", AssetName: "agent", Version: "1.0.0"},
		{BotName: "agent", AssetName: "agent", Version: "1.0.0", Prompt: "   "},
	} {
		if _, err := client.PutAgent(ctx, spec); err == nil {
			t.Fatalf("PutAgent(%+v) succeeded, want validation error", spec)
		}
	}
}

func TestPutAgentRejectsUnknownSkill(t *testing.T) {
	ctx := context.Background()
	_, client := newGitVaultClient(t)
	_, err := client.PutAgent(ctx, AgentSpec{
		BotName:        "reviewer",
		AssetName:      "reviewer",
		Version:        "1.0.0",
		BotDescription: "Reviewer bot.",
		Prompt:         "You are Reviewer.",
		Skills:         []string{"missing-skill"},
	})
	if err == nil || !strings.Contains(err.Error(), "missing-skill") {
		t.Fatalf("PutAgent with unknown skill: err = %v, want error mentioning missing-skill", err)
	}
}

func TestPutAgentRejectsSkillsEntryOfWrongType(t *testing.T) {
	ctx := context.Background()
	_, client := newGitVaultClient(t)

	// Seed an agent — its name must not be usable as a Skills entry.
	if _, err := client.PutAgent(ctx, AgentSpec{
		BotName:        "reviewer",
		AssetName:      "secondary-agent",
		Version:        "1.0.0",
		Description:    "Secondary agent.",
		BotDescription: "Reviewer bot.",
		Prompt:         "You are secondary.",
	}); err != nil {
		t.Fatal(err)
	}
	_, err := client.PutAgent(ctx, AgentSpec{
		BotName:   "reviewer",
		AssetName: "main-agent",
		Version:   "1.0.0",
		Prompt:    "You are main.",
		Skills:    []string{"secondary-agent"},
	})
	if err == nil || !strings.Contains(err.Error(), "secondary-agent") || !strings.Contains(err.Error(), "not skill") {
		t.Fatalf("PutAgent with agent in Skills: err = %v, want wrong-type error", err)
	}
}

func TestEnsureBotRejectsEmptyDescriptionOnCreate(t *testing.T) {
	ctx := context.Background()
	_, client := newGitVaultClient(t)

	// Creating with empty description fails up front rather than producing
	// an anonymous bot that nothing later catches.
	if _, err := client.EnsureBot(ctx, Bot{Name: "anon"}); err == nil || !strings.Contains(err.Error(), "anon") {
		t.Fatalf("EnsureBot create-with-empty-desc: err = %v, want bot-description error", err)
	}
	// Seed the bot, then ensure-bot with empty desc is a no-op (preserve).
	if _, err := client.EnsureBot(ctx, Bot{Name: "anon", Description: "Anon bot."}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.EnsureBot(ctx, Bot{Name: "anon"}); err != nil {
		t.Fatalf("EnsureBot existing-with-empty-desc: %v", err)
	}
}

func TestOpenPathRoundTrip(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	client, err := OpenPath(dir, PathOptions{Actor: Actor{Email: "test@example.com"}})
	if err != nil {
		t.Fatalf("OpenPath: %v", err)
	}
	if err := client.PutSkillZip(ctx, SkillZipSpec{
		Name:    "lint-helper",
		Version: "1.0.0",
		ZipData: skillZip(t, "Lint carefully."),
	}); err != nil {
		t.Fatal(err)
	}
	assertFileContains(t, filepath.Join(dir, "assets", "lint-helper", "1.0.0", "SKILL.md"), "Lint carefully.")

	if _, err := OpenPath("", PathOptions{}); err == nil {
		t.Fatal("OpenPath with empty path succeeded, want error")
	}
	if _, err := OpenPath(filepath.Join(dir, "missing-dir"), PathOptions{}); err == nil {
		t.Fatal("OpenPath against nonexistent dir succeeded, want error")
	}
	// Callers passing a file:// URL (matching the internal vault factory's
	// shape) must not double-prefix and fail.
	urlClient, err := OpenPath("file://"+dir, PathOptions{})
	if err != nil {
		t.Fatalf("OpenPath with file:// prefix: %v", err)
	}
	if urlClient == nil {
		t.Fatal("OpenPath with file:// prefix returned nil client")
	}
}

func TestPutAgentSkillCheckUsesHighestSemver(t *testing.T) {
	ctx := context.Background()
	_, client := newGitVaultClient(t)

	// Publish a skill at 1.0.0, then an agent (same name) at 0.9.0, then a
	// skill again at 2.0.0. list.txt ends in the agent because 0.9.0 was
	// the last append — but the highest semver is 2.0.0, type=skill, so
	// the type check must accept.
	for _, pub := range []struct {
		version string
		isAgent bool
	}{
		{"1.0.0", false},
		{"0.9.0", true},
		{"2.0.0", false},
	} {
		if pub.isAgent {
			if _, err := client.PutAgent(ctx, AgentSpec{
				BotName:        "holder",
				AssetName:      "shape-shifter",
				Version:        pub.version,
				BotDescription: "Holder bot.",
				Prompt:         "agent prompt",
			}); err != nil {
				t.Fatalf("seed agent %s: %v", pub.version, err)
			}
		} else {
			if err := client.PutSkillZip(ctx, SkillZipSpec{
				Name:    "shape-shifter",
				Version: pub.version,
				ZipData: skillZip(t, "skill prompt"),
			}); err != nil {
				t.Fatalf("seed skill %s: %v", pub.version, err)
			}
		}
	}

	// Highest semver (2.0.0) is type=skill, so an agent that lists
	// shape-shifter under Skills must publish cleanly even though list.txt
	// ends in the agent entry.
	if _, err := client.PutAgent(ctx, AgentSpec{
		BotName:        "user",
		AssetName:      "uses-shape-shifter",
		Version:        "1.0.0",
		BotDescription: "User bot.",
		Prompt:         "use prompt",
		Skills:         []string{"shape-shifter"},
	}); err != nil {
		t.Fatalf("PutAgent with semver-resolved skill: %v", err)
	}
}

func TestPutSkillZipRejectsUnknownBot(t *testing.T) {
	ctx := context.Background()
	_, client := newGitVaultClient(t)
	err := client.PutSkillZip(ctx, SkillZipSpec{
		Name:    "lint-helper",
		Version: "1.0.0",
		ZipData: skillZip(t, "Lint carefully."),
		BotName: "phantom",
	})
	if err == nil || !strings.Contains(err.Error(), "phantom") || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("PutSkillZip with unknown bot: err = %v, want bot-not-found error", err)
	}
}

func TestPutAgentPreservesBotDescriptionAcrossPublishes(t *testing.T) {
	ctx := context.Background()
	remote, client := newGitVaultClient(t)

	// First publish seeds the bot description.
	if _, err := client.PutAgent(ctx, AgentSpec{
		BotName:        "reviewer",
		AssetName:      "reviewer-a",
		Version:        "1.0.0",
		Description:    "Agent A description.",
		BotDescription: "Reviews pull requests.",
		Prompt:         "You are A.",
	}); err != nil {
		t.Fatal(err)
	}
	// Second publish provides only an agent Description (no BotDescription).
	// The bot's identity description must be preserved, not blanked.
	if _, err := client.PutAgent(ctx, AgentSpec{
		BotName:     "reviewer",
		AssetName:   "reviewer-b",
		Version:     "1.0.0",
		Description: "Agent B description.",
		Prompt:      "You are B.",
	}); err != nil {
		t.Fatal(err)
	}

	clone := cloneRemote(t, remote)
	manifest := readFile(t, filepath.Join(clone, "sx.toml"))
	if !strings.Contains(manifest, `description = "Reviews pull requests."`) {
		t.Fatalf("bot description was overwritten on second publish:\n%s", manifest)
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

func TestBuildGitClientOptionsAuthTokenRouting(t *testing.T) {
	sshEnv := envFromOptions(t, "git@gitlab.com:org/repo.git", GitOptions{AuthToken: "token"})
	if hasGitBasicAuthEnv(sshEnv) {
		t.Fatalf("SSH remote configured HTTP basic auth env: %v", sshEnv)
	}
	if _, err := buildGitClientOptions("https:///org/repo.git", GitOptions{AuthToken: "token"}); err == nil {
		t.Fatal("malformed HTTPS remote with token succeeded, want error")
	}
	// Same malformed URL with no token must also fail — URL validity is
	// orthogonal to auth, and silently accepting the bad URL would defer
	// the error to git clone time.
	if _, err := buildGitClientOptions("https:///org/repo.git", GitOptions{}); err == nil {
		t.Fatal("malformed HTTPS remote without token succeeded, want error")
	}
	httpEnv := envFromOptions(t, "http://git.example.test/org/repo.git", GitOptions{AuthToken: "token"})
	if !strings.Contains(strings.Join(httpEnv, "\n"), "http.http://git.example.test/.extraheader") {
		t.Fatalf("HTTP remote did not configure HTTP basic auth env: %v", httpEnv)
	}
	// HTTPS is the dominant real-world case; lock in that the basic-auth
	// env header is set for https://github.com and that the default
	// username on this host is "x-access-token".
	githubEnv := envFromOptions(t, "https://github.com/org/repo.git", GitOptions{AuthToken: "token"})
	if !strings.Contains(strings.Join(githubEnv, "\n"), "http.https://github.com/.extraheader") {
		t.Fatalf("HTTPS GitHub remote did not configure HTTP basic auth env: %v", githubEnv)
	}
	user, pass := decodeBasicAuth(t, githubEnv)
	if user != "x-access-token" || pass != "token" {
		t.Fatalf("github.com basic auth = %q:%q, want x-access-token:token", user, pass)
	}
	// gitlab.com must pick the oauth2 username automatically — regression
	// guard on the DefaultHTTPAuthUsername GitLab branch.
	gitlabEnv := envFromOptions(t, "https://gitlab.com/org/repo.git", GitOptions{AuthToken: "token"})
	user, pass = decodeBasicAuth(t, gitlabEnv)
	if user != "oauth2" || pass != "token" {
		t.Fatalf("gitlab.com basic auth = %q:%q, want oauth2:token", user, pass)
	}
	// SSHKeyPath + AuthToken on an HTTPS URL: SSH key wins and the basic
	// auth env must NOT be set, since the underlying git client rewrites
	// the URL to SSH and the basic-auth header would never apply.
	bothEnv := envFromOptions(t, "https://github.com/org/repo.git", GitOptions{
		AuthToken:  "token",
		SSHKeyPath: "/keys/id_ed25519",
	})
	if hasGitBasicAuthEnv(bothEnv) {
		t.Fatalf("SSHKeyPath did not suppress HTTPS basic auth env: %v", bothEnv)
	}
}

func decodeBasicAuth(t *testing.T, env []string) (string, string) {
	t.Helper()
	const prefix = "AUTHORIZATION: basic "
	for _, e := range env {
		_, encoded, ok := strings.Cut(e, prefix)
		if !ok {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			t.Fatalf("decode basic auth %q: %v", encoded, err)
		}
		user, pass, ok := strings.Cut(string(raw), ":")
		if !ok {
			t.Fatalf("basic auth payload missing colon: %q", raw)
		}
		return user, pass
	}
	t.Fatalf("env had no AUTHORIZATION header: %v", env)
	return "", ""
}

func envFromOptions(t *testing.T, repoURL string, opts GitOptions) []string {
	t.Helper()
	gitOpts, err := buildGitClientOptions(repoURL, opts)
	if err != nil {
		t.Fatalf("buildGitClientOptions(%q): %v", repoURL, err)
	}
	return git.NewClientWithOptions(gitOpts...).ExtraEnv()
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

// skillZipWithMetadata produces a skill zip containing both SKILL.md and a
// metadata.toml whose [asset].description is the supplied value. Used to
// exercise normalizeSkillZip's description-merge rule.
func skillZipWithMetadata(t *testing.T, prompt, description string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	skillW, err := zw.Create("SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := skillW.Write([]byte(prompt)); err != nil {
		t.Fatal(err)
	}
	metaW, err := zw.Create("metadata.toml")
	if err != nil {
		t.Fatal(err)
	}
	// The Name/Version/Type fields are overwritten by normalizeSkillZip from
	// SkillZipSpec, so placeholder values are fine here.
	metaBody := "metadata-version = \"1.0\"\n\n" +
		"[asset]\n" +
		"name = \"placeholder\"\n" +
		"version = \"0.0.0\"\n" +
		"type = \"skill\"\n" +
		"description = \"" + description + "\"\n\n" +
		"[skill]\n" +
		"prompt-file = \"SKILL.md\"\n"
	if _, err := metaW.Write([]byte(metaBody)); err != nil {
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
