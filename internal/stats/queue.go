package stats

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/sleuth-io/sx/internal/cache"
	"github.com/sleuth-io/sx/internal/vault"
)

// UsageEvent represents a single asset usage event
type UsageEvent struct {
	AssetName    string `json:"asset_name"`
	AssetVersion string `json:"asset_version"`
	AssetType    string `json:"asset_type"`
	Timestamp    string `json:"timestamp"`
}

// GetQueuePath returns the path to the usage queue directory
func GetQueuePath() string {
	cacheDir, _ := cache.GetCacheDir()
	return filepath.Join(cacheDir, "usage-queue")
}

// EnqueueEvent writes a usage event to the queue directory
func EnqueueEvent(event UsageEvent) error {
	queueDir := GetQueuePath()

	// Create queue directory if it doesn't exist
	if err := os.MkdirAll(queueDir, 0755); err != nil {
		return fmt.Errorf("failed to create queue directory: %w", err)
	}

	// Generate filename: {timestamp}-{uuid}.json
	timestamp := time.Now().Format("20060102-150405")
	filename := fmt.Sprintf("%s-%s.json", timestamp, uuid.New().String())
	filePath := filepath.Join(queueDir, filename)

	// Marshal event to JSON
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	// Write to file
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write queue file: %w", err)
	}

	return nil
}

// DequeueEvents reads up to 'limit' events from the queue directory
// Returns events, file paths, and error
func DequeueEvents(limit int) ([]UsageEvent, []string, error) {
	queueDir := GetQueuePath()

	// Check if queue directory exists
	if _, err := os.Stat(queueDir); os.IsNotExist(err) {
		return nil, nil, nil // No queue directory, no events
	}

	// Read all files in queue directory
	entries, err := os.ReadDir(queueDir)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read queue directory: %w", err)
	}

	// Filter for .json files and sort by name (which includes timestamp)
	var jsonFiles []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			jsonFiles = append(jsonFiles, entry.Name())
		}
	}
	sort.Strings(jsonFiles)

	// Limit number of files to process
	if len(jsonFiles) > limit {
		jsonFiles = jsonFiles[:limit]
	}

	// Read and parse each file
	var events []UsageEvent
	var filePaths []string
	for _, filename := range jsonFiles {
		filePath := filepath.Join(queueDir, filename)

		data, err := os.ReadFile(filePath)
		if err != nil {
			// Log error but continue with other files
			fmt.Fprintf(os.Stderr, "Warning: failed to read queue file %s: %v\n", filename, err)
			continue
		}

		var event UsageEvent
		if err := json.Unmarshal(data, &event); err != nil {
			// Log error but continue with other files
			fmt.Fprintf(os.Stderr, "Warning: failed to parse queue file %s: %v\n", filename, err)
			continue
		}

		events = append(events, event)
		filePaths = append(filePaths, filePath)
	}

	return events, filePaths, nil
}

// DeleteEventFiles deletes the specified event files from the queue
func DeleteEventFiles(filePaths []string) error {
	for _, filePath := range filePaths {
		if err := os.Remove(filePath); err != nil {
			return fmt.Errorf("failed to delete queue file %s: %w", filePath, err)
		}
	}
	return nil
}

// FlushQueue loads pending events from queue and sends them to the repository
func FlushQueue(ctx context.Context, repo vault.Vault) error {
	// Load pending events
	events, filePaths, err := DequeueEvents(100)
	if err != nil {
		return fmt.Errorf("failed to dequeue events: %w", err)
	}

	if len(events) == 0 {
		return nil // No events to flush
	}

	// Format as JSONL
	jsonl := formatAsJSONL(events)

	// POST to repository
	if err := repo.PostUsageStats(ctx, jsonl); err != nil {
		return fmt.Errorf("failed to post usage stats: %w", err)
	}

	// On success, delete processed files
	if err := DeleteEventFiles(filePaths); err != nil {
		return fmt.Errorf("failed to delete processed queue files: %w", err)
	}

	return nil
}

// formatAsJSONL converts events to JSONL format (newline-separated JSON)
func formatAsJSONL(events []UsageEvent) string {
	var lines []string
	for _, event := range events {
		data, err := json.Marshal(event)
		if err != nil {
			// Skip events that fail to marshal
			fmt.Fprintf(os.Stderr, "Warning: failed to marshal event: %v\n", err)
			continue
		}
		lines = append(lines, string(data))
	}
	return strings.Join(lines, "\n")
}
