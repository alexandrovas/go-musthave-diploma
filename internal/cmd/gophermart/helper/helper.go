package helper

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
)

func slogLevel(level string) (slog.Level, error) {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	}
	return slog.LevelInfo, fmt.Errorf("unknown log level: %s", level)
}

// NewLogger создаёт структурированный логгер с заданным уровнем и форматом
func NewLogger(level, format string) (*slog.Logger, error) {
	handlerLevel, err := slogLevel(level)
	if err != nil {
		return nil, err
	}

	opts := &slog.HandlerOptions{Level: handlerLevel}

	var handler slog.Handler
	switch strings.ToLower(format) {
	case "json":
		handler = slog.NewJSONHandler(os.Stdout, opts)
	case "text":
		handler = slog.NewTextHandler(os.Stdout, opts)
	default:
		return nil, fmt.Errorf("unknown log format: %s", format)
	}

	return slog.New(handler), nil
}
