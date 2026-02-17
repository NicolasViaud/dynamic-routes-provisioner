package config

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
)

// TextHandler is a human-readable slog handler that outputs:
//
//	LEVEL [component] message key=value key=value ...
//
// No timestamp. Component is extracted from attributes and displayed in brackets.
type TextHandler struct {
	w     io.Writer
	mu    *sync.Mutex
	level slog.Leveler
	attrs []slog.Attr
	group string
}

// NewTextHandler creates a TextHandler writing to w.
func NewTextHandler(w io.Writer, level slog.Leveler) *TextHandler {
	return &TextHandler{
		w:     w,
		mu:    &sync.Mutex{},
		level: level,
	}
}

func (h *TextHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level.Level()
}

func (h *TextHandler) Handle(_ context.Context, r slog.Record) error {
	var component string
	var extras []slog.Attr

	// Collect pre-set attrs (from With), separating component.
	for _, a := range h.attrs {
		if a.Key == "component" {
			component = a.Value.String()
		} else {
			extras = append(extras, a)
		}
	}

	// Collect record attrs, separating component.
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "component" {
			component = a.Value.String()
		} else {
			extras = append(extras, a)
		}
		return true
	})

	// Build line: LEVEL [component] msg key=value ...
	buf := make([]byte, 0, 256)

	// Level — left-padded to 5 chars for alignment.
	buf = fmt.Appendf(buf, "%-5s ", r.Level.String())

	// Component in brackets.
	if component != "" {
		buf = fmt.Appendf(buf, "[%s] ", component)
	}

	// Message.
	buf = append(buf, r.Message...)

	// Extra attributes.
	for _, a := range extras {
		buf = fmt.Appendf(buf, " %s=%s", a.Key, a.Value.String())
	}

	buf = append(buf, '\n')

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := h.w.Write(buf)
	return err
}

func (h *TextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &TextHandler{
		w:     h.w,
		mu:    h.mu,
		level: h.level,
		attrs: append(cloneAttrs(h.attrs), attrs...),
		group: h.group,
	}
}

func (h *TextHandler) WithGroup(name string) slog.Handler {
	return &TextHandler{
		w:     h.w,
		mu:    h.mu,
		level: h.level,
		attrs: cloneAttrs(h.attrs),
		group: name,
	}
}

func cloneAttrs(attrs []slog.Attr) []slog.Attr {
	if len(attrs) == 0 {
		return nil
	}
	c := make([]slog.Attr, len(attrs))
	copy(c, attrs)
	return c
}
