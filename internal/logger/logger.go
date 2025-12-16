package logger

import (
	"io"
	"log/slog"
	"path/filepath"
	"sync"

	"github.com/sleuth-io/skills/internal/cache"
	"gopkg.in/natefinch/lumberjack.v2"
)

var (
	defaultLogger *slog.Logger
	once          sync.Once
)

// Get returns the global logger instance, initializing it once
func Get() *slog.Logger {
	once.Do(func() {
		defaultLogger = initLogger()
	})
	return defaultLogger
}

// initLogger creates the global logger that writes to sx.log in cache directory
// Uses lumberjack for automatic log rotation to keep file under 100KB
// If the log file cannot be created, returns a no-op logger that discards all output
func initLogger() *slog.Logger {
	cacheDir, err := cache.GetCacheDir()
	if err != nil {
		return slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	logPath := filepath.Join(cacheDir, "sx.log")

	// Use lumberjack for log rotation
	logWriter := &lumberjack.Logger{
		Filename:   logPath,
		MaxSize:    1, // 1 MB max (will keep well under 100KB with MaxBackups=0)
		MaxBackups: 0, // Don't keep old log files
		MaxAge:     0, // Don't delete based on age
		Compress:   false,
	}

	handler := slog.NewTextHandler(logWriter, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})

	return slog.New(handler)
}
