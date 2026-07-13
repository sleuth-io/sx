package main

import (
	"sort"
	"strings"
)

// The draft sheet's "Changes" view: what publishing this draft would change
// relative to the latest published revision of its target asset. Diffs are
// computed here (not in the frontend) so the same engine can serve future
// surfaces (revision history, CLI) and stay under test.

// DiffLine is one row of a rendered diff. Kind is "context", "add" or
// "del"; OldNo/NewNo are 1-based line numbers, 0 on the side the line
// doesn't exist.
type DiffLine struct {
	Kind  string `json:"kind"`
	OldNo int    `json:"oldNo"`
	NewNo int    `json:"newNo"`
	Text  string `json:"text"`
}

// DiffHunk is a contiguous run of changed lines plus surrounding context,
// mirroring a unified-diff @@ header.
type DiffHunk struct {
	OldStart int        `json:"oldStart"`
	OldLines int        `json:"oldLines"`
	NewStart int        `json:"newStart"`
	NewLines int        `json:"newLines"`
	Lines    []DiffLine `json:"lines"`
}

// FileDiff is one file's changes. Status is "added", "modified" or
// "deleted".
type FileDiff struct {
	Path      string     `json:"path"`
	Status    string     `json:"status"`
	Additions int        `json:"additions"`
	Deletions int        `json:"deletions"`
	Hunks     []DiffHunk `json:"hunks"`
}

// DraftDiff is everything the Changes view renders: per-file diffs plus
// whole-draft totals.
type DraftDiff struct {
	Files     []FileDiff `json:"files"`
	Additions int        `json:"additions"`
	Deletions int        `json:"deletions"`
}

// DiffDraft diffs a draft (as currently held by the sheet, saved or not)
// against the latest published files of its target asset. A draft that
// creates a new asset diffs against nothing: every file shows as added.
func (a *App) DiffDraft(d Draft) (DraftDiff, error) {
	base := map[string]string{}
	if d.TargetAsset != "" {
		detail, err := a.GetAsset(d.TargetAsset, "")
		if err != nil {
			return DraftDiff{}, err
		}
		for _, f := range detail.Files {
			if f.Path == "metadata.toml" {
				continue // regenerated on publish; never a user-visible change
			}
			base[f.Path] = f.Content
		}
	}
	return diffFiles(base, d.Files), nil
}

// diffFiles pairs base and draft files by path and diffs each pair.
func diffFiles(base map[string]string, files []AssetFile) DraftDiff {
	draft := map[string]string{}
	for _, f := range files {
		if f.Path == "metadata.toml" {
			continue
		}
		draft[f.Path] = f.Content
	}

	paths := make([]string, 0, len(base)+len(draft))
	for p := range draft {
		paths = append(paths, p)
	}
	for p := range base {
		if _, ok := draft[p]; !ok && p != "metadata.toml" {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)

	var out DraftDiff
	for _, p := range paths {
		before, inBase := base[p]
		after, inDraft := draft[p]
		var fd FileDiff
		switch {
		case !inBase:
			fd = fileDiff(p, "added", "", after)
		case !inDraft:
			fd = fileDiff(p, "deleted", before, "")
		case before != after:
			fd = fileDiff(p, "modified", before, after)
		default:
			continue // unchanged files stay out of the view entirely
		}
		out.Files = append(out.Files, fd)
		out.Additions += fd.Additions
		out.Deletions += fd.Deletions
	}
	return out
}

func fileDiff(path, status, before, after string) FileDiff {
	lines := diffLines(splitLines(before), splitLines(after))
	fd := FileDiff{Path: path, Status: status, Hunks: hunksFrom(lines)}
	for _, l := range lines {
		switch l.Kind {
		case "add":
			fd.Additions++
		case "del":
			fd.Deletions++
		}
	}
	return fd
}

// splitLines splits content into lines without a phantom empty line for a
// trailing newline.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	s = strings.TrimSuffix(s, "\n")
	return strings.Split(s, "\n")
}

// maxDiffCells caps the LCS table. Beyond it (two ~2000-line files that
// share nothing) the whole middle renders as delete-then-add — still
// correct, just less minimal.
const maxDiffCells = 4_000_000

// diffLines produces a full (context-inclusive) line diff of old → new
// using longest-common-subsequence on the middle after trimming the common
// prefix and suffix.
func diffLines(oldLines, newLines []string) []DiffLine {
	// Common prefix.
	prefix := 0
	for prefix < len(oldLines) && prefix < len(newLines) && oldLines[prefix] == newLines[prefix] {
		prefix++
	}
	// Common suffix (of what remains).
	suffix := 0
	for suffix < len(oldLines)-prefix && suffix < len(newLines)-prefix &&
		oldLines[len(oldLines)-1-suffix] == newLines[len(newLines)-1-suffix] {
		suffix++
	}

	var out []DiffLine
	oldNo, newNo := 1, 1
	emit := func(kind, text string) {
		l := DiffLine{Kind: kind, Text: text}
		if kind != "add" {
			l.OldNo = oldNo
			oldNo++
		}
		if kind != "del" {
			l.NewNo = newNo
			newNo++
		}
		out = append(out, l)
	}

	for i := range prefix {
		emit("context", oldLines[i])
	}

	midOld := oldLines[prefix : len(oldLines)-suffix]
	midNew := newLines[prefix : len(newLines)-suffix]
	if len(midOld)*len(midNew) > maxDiffCells {
		for _, l := range midOld {
			emit("del", l)
		}
		for _, l := range midNew {
			emit("add", l)
		}
	} else {
		for _, step := range lcsDiff(midOld, midNew) {
			emit(step.kind, step.text)
		}
	}

	for i := len(oldLines) - suffix; i < len(oldLines); i++ {
		emit("context", oldLines[i])
	}
	return out
}

type diffStep struct {
	kind string
	text string
}

// lcsDiff walks a longest-common-subsequence table to produce the minimal
// del/add/context sequence for two line slices.
func lcsDiff(a, b []string) []diffStep {
	m, n := len(a), len(b)
	// lcs[i][j] = LCS length of a[i:] and b[j:].
	lcs := make([][]int, m+1)
	for i := range lcs {
		lcs[i] = make([]int, n+1)
	}
	for i := m - 1; i >= 0; i-- {
		for j := n - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}

	steps := make([]diffStep, 0, m+n)
	i, j := 0, 0
	for i < m && j < n {
		switch {
		case a[i] == b[j]:
			steps = append(steps, diffStep{"context", a[i]})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			steps = append(steps, diffStep{"del", a[i]})
			i++
		default:
			steps = append(steps, diffStep{"add", b[j]})
			j++
		}
	}
	for ; i < m; i++ {
		steps = append(steps, diffStep{"del", a[i]})
	}
	for ; j < n; j++ {
		steps = append(steps, diffStep{"add", b[j]})
	}
	return steps
}

// hunkContext is how many unchanged lines surround each run of changes;
// runs closer than 2*hunkContext merge into one hunk, as in git.
const hunkContext = 3

// hunksFrom groups a full line diff into unified-diff style hunks.
func hunksFrom(lines []DiffLine) []DiffHunk {
	var hunks []DiffHunk
	i := 0
	for i < len(lines) {
		if lines[i].Kind == "context" {
			i++
			continue
		}
		// Found a change; open a hunk hunkContext lines back.
		start := max(i-hunkContext, 0)
		// Extend to the last change whose gap to the next change is small
		// enough that the hunks would overlap.
		end := i // exclusive index just past the last change so far
		for j := i; j < len(lines); j++ {
			if lines[j].Kind != "context" {
				end = j + 1
			} else if j-end >= 2*hunkContext {
				break
			}
		}
		stop := min(end+hunkContext, len(lines))

		hunk := DiffHunk{Lines: lines[start:stop]}
		for _, l := range hunk.Lines {
			if l.Kind != "add" {
				if hunk.OldStart == 0 {
					hunk.OldStart = l.OldNo
				}
				hunk.OldLines++
			}
			if l.Kind != "del" {
				if hunk.NewStart == 0 {
					hunk.NewStart = l.NewNo
				}
				hunk.NewLines++
			}
		}
		hunks = append(hunks, hunk)
		i = stop
	}
	return hunks
}
