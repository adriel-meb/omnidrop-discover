package main

import (
	"log/slog"
	"os"
)

// newLogger returns a structured logger. When jsonOutput is true it uses JSON
// format (useful when consuming logs programmatically); otherwise it uses
// human-readable text. The verbose flag enables Debug-level output.
func newLogger(jsonOutput, verbose bool) *slog.Logger {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	opts := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	if jsonOutput {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	} else {
		handler = slog.NewTextHandler(os.Stderr, opts)
	}
	return slog.New(handler)
}
