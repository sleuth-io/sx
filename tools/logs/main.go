// tools/logs is a dev tool for viewing sx logs with colors
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ANSI color codes
const (
	reset      = "\033[0m"
	green      = "\033[32m"
	cyan       = "\033[36m"
	magenta    = "\033[35m"
	boldRed    = "\033[1;31m"
	boldYellow = "\033[1;33m"
)

func main() {
	lines := flag.Int("n", 20, "number of lines to show before following")
	flag.Parse()

	logPath := getLogPath()
	if logPath == "" {
		fmt.Fprintln(os.Stderr, "Could not determine log path")
		os.Exit(1)
	}

	// Always show last N lines, then follow
	showLastLines(logPath, *lines)
	followFile(logPath)
}

func getLogPath() string {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(cacheDir, "sx", "sx.log")
}

func showLastLines(path string, n int) {
	file, err := os.Open(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening log: %v\n", err)
		os.Exit(1)
	}
	defer file.Close()

	// Read all lines (simple approach for small log files)
	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	// Show last n lines
	start := max(len(lines)-n, 0)
	for _, line := range lines[start:] {
		fmt.Println(colorizeLine(line))
	}
}

func followFile(path string) {
	file, err := os.Open(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening log: %v\n", err)
		os.Exit(1)
	}
	defer file.Close()

	// Seek to end
	file.Seek(0, io.SeekEnd)

	reader := bufio.NewReader(file)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				time.Sleep(100 * time.Millisecond)
				continue
			}
			return
		}
		fmt.Print(colorizeLine(strings.TrimRight(line, "\n")))
		fmt.Println()
	}
}

// colorizeLine parses a slog text or JSON line and adds colors
// Text format: time=2024-01-15T10:30:00Z level=INFO msg="hello" key=value
// JSON format: {"time":"...","level":"INFO","msg":"hello","key":"value"}
func colorizeLine(line string) string {
	var level, timeVal, msg string
	var extra map[string]any

	// Detect JSON format
	if strings.HasPrefix(strings.TrimSpace(line), "{") {
		level, timeVal, msg, extra = parseJSONLine(line)
	} else {
		level = extractValue(line, "level")
		timeVal = extractValue(line, "time")
		msg = extractValue(line, "msg")
	}

	levelColor := getLevelColor(level)

	// Build colored output
	var sb strings.Builder

	// Time (cyan)
	if timeVal != "" {
		// Show just time part if it's a full timestamp
		if len(timeVal) > 11 {
			timeVal = timeVal[11:19] // Extract HH:MM:SS
		}
		sb.WriteString(cyan)
		sb.WriteString(timeVal)
		sb.WriteString(reset)
		sb.WriteString(" ")
	}

	// Level (colored)
	if level != "" {
		sb.WriteString(levelColor)
		sb.WriteString(levelShort(level))
		sb.WriteString(reset)
		sb.WriteString(" ")
	}

	// Message
	if msg != "" {
		sb.WriteString(msg)
	}

	// Remaining key=value pairs
	if extra != nil {
		// JSON format - use parsed extra fields
		if len(extra) > 0 {
			sb.WriteString(" -- ")
			first := true
			for k, v := range extra {
				if !first {
					sb.WriteString(" ")
				}
				first = false
				fmt.Fprintf(&sb, "%s=%v", k, v)
			}
		}
	} else {
		// Text format - extract remaining
		remaining := extractRemaining(line, []string{"time", "level", "msg"})
		if remaining != "" {
			sb.WriteString(" -- ")
			sb.WriteString(remaining)
		}
	}

	return sb.String()
}

// parseJSONLine parses a JSON log line and returns level, time, msg, and extra fields
func parseJSONLine(line string) (level, timeVal, msg string, extra map[string]any) {
	var data map[string]any
	if err := json.Unmarshal([]byte(line), &data); err != nil {
		return "", "", line, nil // Return line as-is if parse fails
	}

	if v, ok := data["level"].(string); ok {
		level = v
		delete(data, "level")
	}
	if v, ok := data["time"].(string); ok {
		timeVal = v
		delete(data, "time")
	}
	if v, ok := data["msg"].(string); ok {
		msg = v
		delete(data, "msg")
	}

	if len(data) > 0 {
		extra = data
	}
	return
}

func extractValue(line, key string) string {
	// Match key=value or key="value with spaces"
	pattern := regexp.MustCompile(key + `=(?:"([^"]*)"|([\S]+))`)
	match := pattern.FindStringSubmatch(line)
	if len(match) >= 2 {
		if match[1] != "" {
			return match[1]
		}
		if len(match) >= 3 {
			return match[2]
		}
	}
	return ""
}

func extractRemaining(line string, exclude []string) string {
	// Remove excluded key=value pairs and return the rest
	result := line
	for _, key := range exclude {
		pattern := regexp.MustCompile(key + `=(?:"[^"]*"|\S+)\s*`)
		result = pattern.ReplaceAllString(result, "")
	}
	return strings.TrimSpace(result)
}

func getLevelColor(level string) string {
	switch strings.ToUpper(level) {
	case "ERROR":
		return boldRed
	case "WARN", "WARNING":
		return boldYellow
	case "INFO":
		return green
	case "DEBUG":
		return magenta
	default:
		return ""
	}
}

func levelShort(level string) string {
	switch strings.ToUpper(level) {
	case "ERROR":
		return "ERR"
	case "WARNING":
		return "WRN"
	case "INFO":
		return "INF"
	case "DEBUG":
		return "DBG"
	default:
		return level
	}
}
