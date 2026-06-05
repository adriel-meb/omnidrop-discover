package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"
)

// newLogger returns a modern, visually clean logger. When jsonOutput is true
// it uses JSON format (for programmatic consumption); otherwise it uses a
// custom colorized human-readable format.
func newLogger(jsonOutput, verbose bool) *slog.Logger {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}

	var handler slog.Handler
	if jsonOutput {
		handler = slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	} else {
		handler = newModernHandler(os.Stderr, level)
	}
	return slog.New(handler)
}

// ─── Modern handler ───────────────────────────────────────────────────────────

// modernHandler is a custom slog.Handler that produces compact, colorized,
// human-readable log output.
type modernHandler struct {
	mu     sync.Mutex
	w      io.Writer
	level  slog.Level
	attrs  []slog.Attr
	groups []string
}

func newModernHandler(w io.Writer, level slog.Level) *modernHandler {
	return &modernHandler{w: w, level: level}
}

func (h *modernHandler) Enabled(_ context.Context, l slog.Level) bool {
	return l >= h.level
}

func (h *modernHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	icon, label := levelStyle(r.Level)
	ts := r.Time.Format("15:04:05")

	var parts []string
	parts = append(parts, dim(ts))
	parts = append(parts, icon)
	parts = append(parts, label)

	if msg := r.Message; msg != "" {
		parts = append(parts, msg)
	}

	attrs := make([]slog.Attr, 0, r.NumAttrs())
	r.Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, a)
		return true
	})
	attrs = append(attrs, h.attrs...)

	for _, a := range attrs {
		if a.Value.Kind() == slog.KindGroup {
			for _, ga := range a.Value.Group() {
				parts = append(parts, fmt.Sprintf("  %s=%s", a.Key+"."+ga.Key, formatValue(ga.Value)))
			}
		} else {
			parts = append(parts, fmt.Sprintf("  %s=%s", a.Key, formatValue(a.Value)))
		}
	}

	fmt.Fprintln(h.w, strings.Join(parts, " "))
	return nil
}

func (h *modernHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	h2 := h.clone()
	h2.attrs = append(h2.attrs, attrs...)
	return h2
}

func (h *modernHandler) WithGroup(name string) slog.Handler {
	h2 := h.clone()
	h2.groups = append(h2.groups, name)
	return h2
}

func (h *modernHandler) clone() *modernHandler {
	return &modernHandler{
		w:      h.w,
		level:  h.level,
		attrs:  append([]slog.Attr{}, h.attrs...),
		groups: append([]string{}, h.groups...),
	}
}

// ─── Level styles ─────────────────────────────────────────────────────────────

func levelStyle(l slog.Level) (icon, label string) {
	switch {
	case l < slog.LevelInfo:
		return cyan("◷"), cyan("debug")
	case l < slog.LevelWarn:
		return green("●"), green("done")
	case l < slog.LevelError:
		return yellow("▲"), yellow("warn")
	default:
		return red("✗"), red("fail")
	}
}

// ─── Value formatters ─────────────────────────────────────────────────────────

func formatValue(v slog.Value) string {
	switch v.Kind() {
	case slog.KindString:
		return v.String()
	case slog.KindDuration:
		d := v.Duration()
		if d >= time.Second {
			return fmt.Sprintf("%.1fs", d.Seconds())
		}
		return fmt.Sprintf("%dms", d.Milliseconds())
	case slog.KindTime:
		return v.Time().Format("15:04:05")
	case slog.KindGroup:
		var b strings.Builder
		for _, a := range v.Group() {
			if b.Len() > 0 {
				b.WriteByte(' ')
			}
			fmt.Fprintf(&b, "%s=%s", a.Key, formatValue(a.Value))
		}
		return b.String()
	default:
		return v.String()
	}
}

// ─── ANSI helpers ─────────────────────────────────────────────────────────────

func red(s string) string     { return "\033[31m" + s + "\033[0m" }
func green(s string) string   { return "\033[32m" + s + "\033[0m" }
func yellow(s string) string  { return "\033[33m" + s + "\033[0m" }
func cyan(s string) string    { return "\033[36m" + s + "\033[0m" }
func dim(s string) string     { return "\033[2m" + s + "\033[0m" }
