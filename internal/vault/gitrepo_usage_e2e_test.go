package vault

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sleuth-io/sx/internal/mgmt"
)

// TestGitVault_RecordUsageEvents_PushesToRemote covers the regression
// reported when usage events were appended locally but never made it
// to the vault remote. It seeds a bare git repo as the "remote",
// records usage events through GitVault, and asserts that the events
// land in a pushed commit on the bare repo. Then it records again
// immediately and asserts the throttle suppresses the second push,
// and finally rewinds the sentinel mtime to confirm the next call
// after the throttle window does push.
func TestGitVault_RecordUsageEvents_PushesToRemote(t *testing.T) {
	mgmt.ResetActorCache()

	cacheDir := t.TempDir()
	t.Setenv("SX_CACHE_DIR", cacheDir)

	remoteDir := filepath.Join(t.TempDir(), "vault.git")
	gitRun(t, "", "init", "--bare", "-b", "main", remoteDir)

	// Seed an initial commit so cloneOrUpdate has something to pull.
	seedDir := filepath.Join(t.TempDir(), "seed")
	gitRun(t, "", "init", "-b", "main", seedDir)
	gitRun(t, seedDir, "config", "user.email", "seed@example.com")
	gitRun(t, seedDir, "config", "user.name", "Seed")
	if err := os.WriteFile(filepath.Join(seedDir, "README.md"), []byte("seed\n"), 0644); err != nil {
		t.Fatalf("write seed README: %v", err)
	}
	gitRun(t, seedDir, "add", ".")
	gitRun(t, seedDir, "commit", "-m", "seed")
	gitRun(t, seedDir, "remote", "add", "origin", remoteDir)
	gitRun(t, seedDir, "push", "origin", "main")

	repoURL := "file://" + remoteDir
	v, err := NewGitVault(repoURL)
	if err != nil {
		t.Fatalf("NewGitVault: %v", err)
	}

	ctx := context.Background()

	// Force the initial clone so we can pin git identity inside it
	// before the actor lookup runs.
	if _, _, _, err := v.GetLockFile(ctx, ""); err != nil {
		t.Fatalf("GetLockFile (forces clone): %v", err)
	}
	gitRun(t, v.repoPath, "config", "user.email", "alice@example.com")
	gitRun(t, v.repoPath, "config", "user.name", "Alice")

	// 1. First record-usage call should push to the remote.
	events := []mgmt.UsageEvent{
		{Actor: "alice@example.com", AssetName: "my-skill", AssetVersion: "1.0.0", AssetType: "skill"},
	}
	if err := v.RecordUsageEvents(ctx, events); err != nil {
		t.Fatalf("RecordUsageEvents (first): %v", err)
	}

	if !remoteHasUsageCommit(t, remoteDir) {
		t.Fatal("expected remote to have a usage commit after first RecordUsageEvents")
	}
	commitsAfterFirst := remoteCommitCount(t, remoteDir)

	// 2. A second call inside the throttle window must NOT produce a
	//    new commit on the remote.
	events2 := []mgmt.UsageEvent{
		{Actor: "alice@example.com", AssetName: "my-skill", AssetVersion: "1.0.0", AssetType: "skill"},
	}
	if err := v.RecordUsageEvents(ctx, events2); err != nil {
		t.Fatalf("RecordUsageEvents (throttled): %v", err)
	}
	if got := remoteCommitCount(t, remoteDir); got != commitsAfterFirst {
		t.Fatalf("expected throttled call to not push; commits before=%d after=%d",
			commitsAfterFirst, got)
	}

	// 3. Rewind the sentinel mtime past the throttle window — next
	//    call should push again.
	sentinel, err := v.usagePushSentinelPath()
	if err != nil {
		t.Fatalf("usagePushSentinelPath: %v", err)
	}
	old := time.Now().Add(-2 * usagePushInterval)
	if err := os.Chtimes(sentinel, old, old); err != nil {
		t.Fatalf("rewind sentinel mtime: %v", err)
	}

	events3 := []mgmt.UsageEvent{
		{Actor: "alice@example.com", AssetName: "other-skill", AssetVersion: "1.0.0", AssetType: "skill"},
	}
	if err := v.RecordUsageEvents(ctx, events3); err != nil {
		t.Fatalf("RecordUsageEvents (after window): %v", err)
	}
	if got := remoteCommitCount(t, remoteDir); got <= commitsAfterFirst {
		t.Fatalf("expected post-throttle call to push; commits before=%d after=%d",
			commitsAfterFirst, got)
	}

	// Sanity: cache dir is the one we set, so the sentinel lives there.
	if !strings.HasPrefix(sentinel, cacheDir) {
		t.Fatalf("sentinel %q expected to live under cache dir %q", sentinel, cacheDir)
	}
	// And it's not under the working tree.
	if strings.HasPrefix(sentinel, v.repoPath+string(os.PathSeparator)) {
		t.Fatalf("sentinel %q must not live inside the working tree", sentinel)
	}
}

// TestGitVault_RecordUsageEvents_ConcurrentWriterRemoteAhead is a
// regression test for the multi-writer dirty-tree race. Setup:
//   - Local clone is at commit A, which seeded the monthly usage file.
//   - A concurrent writer pushes commit B touching the same monthly
//     file before we run record-usage again.
//   - We then call RecordUsageEvents in a fresh-process state
//     (hasSynced=false) so a pull is required.
//
// Pre-fix ordering: append → pull. The pull would refuse to merge B
// because our local monthly file is dirty, RecordUsageEvents would
// log a warning and bail, the sentinel would never advance, and
// pushes would stall on every subsequent call. Post-fix: pull → append
// → push, so B merges into a clean tree and our append + push lands.
func TestGitVault_RecordUsageEvents_ConcurrentWriterRemoteAhead(t *testing.T) {
	mgmt.ResetActorCache()

	cacheDir := t.TempDir()
	t.Setenv("SX_CACHE_DIR", cacheDir)

	remoteDir := filepath.Join(t.TempDir(), "vault.git")
	gitRun(t, "", "init", "--bare", "-b", "main", remoteDir)

	monthFile := time.Now().UTC().Format("2006-01") + ".jsonl"
	monthRel := filepath.Join(".sx", "usage", monthFile)

	// Seed commit A: monthly usage file with one entry.
	seedDir := filepath.Join(t.TempDir(), "seed")
	gitRun(t, "", "init", "-b", "main", seedDir)
	gitRun(t, seedDir, "config", "user.email", "seed@example.com")
	gitRun(t, seedDir, "config", "user.name", "Seed")
	if err := os.MkdirAll(filepath.Join(seedDir, ".sx", "usage"), 0755); err != nil {
		t.Fatalf("mkdir seed usage: %v", err)
	}
	seedLine := `{"ts":"2026-01-01T00:00:00Z","actor":"seed@example.com","asset_name":"seed-skill","asset_version":"1.0.0","asset_type":"skill"}` + "\n"
	if err := os.WriteFile(filepath.Join(seedDir, monthRel), []byte(seedLine), 0644); err != nil {
		t.Fatalf("write seed monthly file: %v", err)
	}
	gitRun(t, seedDir, "add", ".")
	gitRun(t, seedDir, "commit", "-m", "seed monthly usage")
	gitRun(t, seedDir, "remote", "add", "origin", remoteDir)
	gitRun(t, seedDir, "push", "origin", "main")

	repoURL := "file://" + remoteDir
	v, err := NewGitVault(repoURL)
	if err != nil {
		t.Fatalf("NewGitVault: %v", err)
	}

	ctx := context.Background()

	// Prime: clone the remote into our cache so we land at commit A.
	if _, _, _, err := v.GetLockFile(ctx, ""); err != nil {
		t.Fatalf("GetLockFile (priming clone): %v", err)
	}
	gitRun(t, v.repoPath, "config", "user.email", "alice@example.com")
	gitRun(t, v.repoPath, "config", "user.name", "Alice")

	// Concurrent-writer commit B: append another line to the same
	// monthly file from the seed clone and push. After this push, our
	// clone at v.repoPath is one commit behind on a file that
	// record-usage is about to touch.
	concurrentLine := `{"ts":"2026-01-02T00:00:00Z","actor":"bob@example.com","asset_name":"seed-skill","asset_version":"1.0.0","asset_type":"skill"}` + "\n"
	f, err := os.OpenFile(filepath.Join(seedDir, monthRel), os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("open seed monthly file for append: %v", err)
	}
	if _, err := f.WriteString(concurrentLine); err != nil {
		t.Fatalf("append concurrent line: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close seed monthly file: %v", err)
	}
	gitRun(t, seedDir, "add", monthRel)
	gitRun(t, seedDir, "commit", "-m", "concurrent writer adds line")
	gitRun(t, seedDir, "push", "origin", "main")

	// Fresh-process semantics: drop the sync latch so RecordUsageEvents
	// must actually pull commit B.
	v.hasSynced = false

	events := []mgmt.UsageEvent{
		{Actor: "alice@example.com", AssetName: "my-skill", AssetVersion: "1.0.0", AssetType: "skill"},
	}
	if err := v.RecordUsageEvents(ctx, events); err != nil {
		t.Fatalf("RecordUsageEvents with concurrent writer: %v", err)
	}

	if !remoteHasUsageCommit(t, remoteDir) {
		t.Fatal("expected remote to receive a usage commit when remote was ahead on monthly file")
	}
	// And the concurrent writer's line must still be present on the
	// remote — i.e. we merged, not overwrote.
	headFile := gitOut(t, remoteDir, "show", "HEAD~1:"+monthRel)
	if !strings.Contains(headFile, "bob@example.com") {
		t.Fatalf("expected concurrent writer's line preserved on remote; got %q", headFile)
	}
}

// TestGitVault_RecordUsageEvents_ThrottledThenConcurrentWriter is a
// regression test for the steady-state production scenario flagged
// in PR review: under multi-writer load each `report-usage` is a
// fresh process, so a previous throttled call must NOT leave the
// working tree dirty. Sequence:
//
//  1. Record (push) — sentinel set; tree clean afterwards.
//  2. Record (throttled, fresh process) — commit must still happen
//     so the tree stays clean; only the push is suppressed.
//  3. Concurrent writer pushes a commit touching the same monthly
//     file.
//  4. Record again (still fresh process, sentinel rewound past the
//     window) — pull must succeed against a clean tree, the queued
//     local commits must merge with the concurrent writer's, and
//     the final push must land.
//
// Pre-fix maybePushUsage skipped the commit when throttled, leaving
// .sx/usage/YYYY-MM.jsonl dirty across processes. Step 4's pull
// would then fail with "local changes would be overwritten" and
// pushes would stall indefinitely.
func TestGitVault_RecordUsageEvents_ThrottledThenConcurrentWriter(t *testing.T) {
	mgmt.ResetActorCache()

	cacheDir := t.TempDir()
	t.Setenv("SX_CACHE_DIR", cacheDir)

	remoteDir := filepath.Join(t.TempDir(), "vault.git")
	gitRun(t, "", "init", "--bare", "-b", "main", remoteDir)

	monthFile := time.Now().UTC().Format("2006-01") + ".jsonl"
	monthRel := filepath.Join(".sx", "usage", monthFile)

	// Initial seed so the remote has a main branch to pull from.
	seedDir := filepath.Join(t.TempDir(), "seed")
	gitRun(t, "", "init", "-b", "main", seedDir)
	gitRun(t, seedDir, "config", "user.email", "seed@example.com")
	gitRun(t, seedDir, "config", "user.name", "Seed")
	if err := os.WriteFile(filepath.Join(seedDir, "README.md"), []byte("seed\n"), 0644); err != nil {
		t.Fatalf("write seed README: %v", err)
	}
	gitRun(t, seedDir, "add", ".")
	gitRun(t, seedDir, "commit", "-m", "seed")
	gitRun(t, seedDir, "remote", "add", "origin", remoteDir)
	gitRun(t, seedDir, "push", "origin", "main")

	repoURL := "file://" + remoteDir
	v, err := NewGitVault(repoURL)
	if err != nil {
		t.Fatalf("NewGitVault: %v", err)
	}
	ctx := context.Background()

	if _, _, _, err := v.GetLockFile(ctx, ""); err != nil {
		t.Fatalf("GetLockFile (priming clone): %v", err)
	}
	gitRun(t, v.repoPath, "config", "user.email", "alice@example.com")
	gitRun(t, v.repoPath, "config", "user.name", "Alice")

	// Step 1: first record-usage pushes and sets the sentinel.
	if err := v.RecordUsageEvents(ctx, []mgmt.UsageEvent{
		{Actor: "alice@example.com", AssetName: "skill-a", AssetVersion: "1.0.0", AssetType: "skill"},
	}); err != nil {
		t.Fatalf("RecordUsageEvents step 1: %v", err)
	}
	if dirty := workingTreeStatus(t, v.repoPath); dirty != "" {
		t.Fatalf("expected working tree clean after step 1, got:\n%s", dirty)
	}

	// Step 2: simulate a fresh process inside the throttle window.
	// Sentinel is fresh, so push is suppressed; the commit must still
	// happen so the tree stays clean.
	v.hasSynced = false
	if err := v.RecordUsageEvents(ctx, []mgmt.UsageEvent{
		{Actor: "alice@example.com", AssetName: "skill-b", AssetVersion: "1.0.0", AssetType: "skill"},
	}); err != nil {
		t.Fatalf("RecordUsageEvents step 2 (throttled): %v", err)
	}
	if dirty := workingTreeStatus(t, v.repoPath); dirty != "" {
		t.Fatalf("expected working tree clean after throttled step 2 — dirty tree wedges future pulls, got:\n%s", dirty)
	}

	// Step 3: concurrent writer pushes a commit touching the same
	// monthly file via the seed clone.
	gitRun(t, seedDir, "pull", "origin", "main")
	if err := os.MkdirAll(filepath.Join(seedDir, ".sx", "usage"), 0755); err != nil {
		t.Fatalf("mkdir seed usage: %v", err)
	}
	concurrentLine := `{"ts":"2026-05-08T00:00:00Z","actor":"bob@example.com","asset_name":"skill-c","asset_version":"1.0.0","asset_type":"skill"}` + "\n"
	f, err := os.OpenFile(filepath.Join(seedDir, monthRel), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("open seed monthly file for append: %v", err)
	}
	if _, err := f.WriteString(concurrentLine); err != nil {
		t.Fatalf("append concurrent line: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close seed monthly file: %v", err)
	}
	gitRun(t, seedDir, "add", monthRel)
	gitRun(t, seedDir, "commit", "-m", "concurrent writer adds line")
	gitRun(t, seedDir, "push", "origin", "main")

	// Step 4: rewind the sentinel past the throttle window AND drop
	// hasSynced so the next call must pull. Pre-fix the dirty tree
	// from step 2 would now wedge cloneOrUpdate; post-fix the tree
	// is clean and the pull succeeds.
	sentinel, err := v.usagePushSentinelPath()
	if err != nil {
		t.Fatalf("usagePushSentinelPath: %v", err)
	}
	old := time.Now().Add(-2 * usagePushInterval)
	if err := os.Chtimes(sentinel, old, old); err != nil {
		t.Fatalf("rewind sentinel mtime: %v", err)
	}
	v.hasSynced = false

	if err := v.RecordUsageEvents(ctx, []mgmt.UsageEvent{
		{Actor: "alice@example.com", AssetName: "skill-d", AssetVersion: "1.0.0", AssetType: "skill"},
	}); err != nil {
		t.Fatalf("RecordUsageEvents step 4 (concurrent writer ahead): %v", err)
	}

	// Remote must contain the concurrent writer's line AND alice's
	// queued events; otherwise we either lost a merge or never pushed.
	finalRemote := gitOut(t, remoteDir, "show", "HEAD:"+monthRel)
	if !strings.Contains(finalRemote, "bob@example.com") {
		t.Fatalf("concurrent writer's line missing from remote after merge:\n%s", finalRemote)
	}
	for _, asset := range []string{"skill-a", "skill-b", "skill-d"} {
		if !strings.Contains(finalRemote, asset) {
			t.Fatalf("expected %s in pushed monthly file:\n%s", asset, finalRemote)
		}
	}
}

// workingTreeStatus returns the porcelain status of the .sx/usage
// path inside the given working tree (empty when clean). The full
// tree may legitimately contain other untracked files (e.g.
// sx.toml synthesized by GetLockFile during priming) — those don't
// wedge a pull. The bug we're guarding against is uncommitted
// changes on the monthly JSONL specifically, since that's the file
// concurrent writers also touch.
func workingTreeStatus(t *testing.T, repoPath string) string {
	t.Helper()
	return strings.TrimSpace(gitOut(t, repoPath, "status", "--porcelain", "--", ".sx/usage"))
}

// remoteHasUsageCommit returns true when the remote bare repo contains
// a commit whose subject matches the pusher's "Record usage events"
// message.
func remoteHasUsageCommit(t *testing.T, bareDir string) bool {
	t.Helper()
	out := gitOut(t, bareDir, "log", "--pretty=%s")
	for line := range strings.SplitSeq(out, "\n") {
		if strings.TrimSpace(line) == "Record usage events" {
			return true
		}
	}
	return false
}

func remoteCommitCount(t *testing.T, bareDir string) int {
	t.Helper()
	out := strings.TrimSpace(gitOut(t, bareDir, "rev-list", "--count", "HEAD"))
	n, err := strconv.Atoi(out)
	if err != nil {
		t.Fatalf("unexpected rev-list output %q: %v", out, err)
	}
	return n
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s failed in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return string(out)
}
