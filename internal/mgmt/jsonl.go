package mgmt

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// appendJSONL serializes each item as JSON and appends it to the given
// file with a trailing newline. Parent directories are not created — the
// caller is expected to have ensured them. A single O_APPEND handle is
// reused across items in one call to minimize syscalls; on POSIX this
// also means concurrent process appends don't interleave within a line.
func appendJSONL[T any](path string, items []T) error {
	if len(items) == 0 {
		return nil
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	for _, item := range items {
		line, err := json.Marshal(item)
		if err != nil {
			return fmt.Errorf("failed to marshal jsonl entry: %w", err)
		}
		if _, err := f.Write(append(line, '\n')); err != nil {
			return fmt.Errorf("failed to write jsonl entry: %w", err)
		}
	}
	return nil
}

// readJSONL reads a single JSONL file and returns each line parsed into
// T. Empty lines are skipped. Malformed lines return an error that
// preserves the file basename for easier debugging.
func readJSONL[T any](path string) ([]T, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	var out []T
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var item T
		if err := json.Unmarshal(line, &item); err != nil {
			return nil, fmt.Errorf("malformed jsonl line in %s: %w", filepath.Base(path), err)
		}
		out = append(out, item)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", path, err)
	}
	return out, nil
}

// readMonthlyJSONLDir reads every *.jsonl file in dir (in lexical order,
// which is monotonically increasing month order given the YYYY-MM naming)
// and returns the concatenated events. A non-existent directory returns
// (nil, nil).
func readMonthlyJSONLDir[T any](dir string) ([]T, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read directory %s: %w", dir, err)
	}

	var files []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		files = append(files, filepath.Join(dir, e.Name()))
	}
	sort.Strings(files)

	var all []T
	for _, path := range files {
		items, err := readJSONL[T](path)
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
	}
	return all, nil
}
