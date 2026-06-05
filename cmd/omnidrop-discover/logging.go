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

	ts := dim(r.Time.Format("15:04:05"))
	badge, msgColor := levelBadge(r.Level)

	// Collect attrs.
	attrs := make([]slog.Attr, 0, r.NumAttrs())
	r.Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, a)
		return true
	})
	attrs = append(attrs, h.attrs...)

	// Render:  timestamp │ badge  message  · attr  · attr …
	var parts []string
	parts = append(parts, ts, "│", badge)

	if msg := r.Message; msg != "" {
		parts = append(parts, msgColor+msg+"\033[0m")
	}

	for _, a := range attrs {
		parts = append(parts, attrFmt(a))
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

// ─── Attribute formatter ──────────────────────────────────────────────────────

func attrFmt(a slog.Attr) string {
	if a.Value.Kind() == slog.KindGroup {
		var segs []string
		for _, ga := range a.Value.Group() {
			segs = append(segs, fmt.Sprintf("%s%s%s%s",
				dim(a.Key+"."+ga.Key), dim("="), "", formatValue(ga.Value)))
		}
		return strings.Join(segs, " ")
	}
	return fmt.Sprintf(" %s %s%s%s",
		dim("·"),
		dim(a.Key+"="),
		"",
		formatValue(a.Value),
	)
}

// ─── Level badges ─────────────────────────────────────────────────────────────

func levelBadge(l slog.Level) (badge, msgColor string) {
	switch {
	case l < slog.LevelInfo:
		return debugBadge(), "\033[36m"
	case l < slog.LevelWarn:
		return doneBadge(), "\033[32m"
	case l < slog.LevelError:
		return warnBadge(), "\033[33m"
	default:
		return failBadge(), "\033[31m"
	}
}

func doneBadge() string {
	return "\033[42m\033[30m DONE \033[0m"
}
func warnBadge() string {
	return "\033[43m\033[30m WARN \033[0m"
}
func failBadge() string {
	return "\033[41m\033[97m FAIL \033[0m"
}
func debugBadge() string {
	return "\033[46m\033[30m DEBUG \033[0m"
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

func dim(s string) string   { return "\033[2m" + s + "\033[0m" }
