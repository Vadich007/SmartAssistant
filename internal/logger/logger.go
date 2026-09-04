// Package logger настраивает структурированное логирование.
package logger

import (
	"io"
	"log/slog"
	"strings"
)

// New создаёт логгер по строковым настройкам уровня и формата.
func New(w io.Writer, level, format string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: parseLevel(level)}

	var handler slog.Handler
	if strings.EqualFold(format, "json") {
		handler = slog.NewJSONHandler(w, opts)
	} else {
		handler = slog.NewTextHandler(w, opts)
	}
	return slog.New(handler)
}

// Component возвращает дочерний логгер с меткой компонента.
func Component(l *slog.Logger, name string) *slog.Logger {
	return l.With(slog.String("component", name))
}

// Discard возвращает логгер, который ничего не пишет.
func Discard() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
