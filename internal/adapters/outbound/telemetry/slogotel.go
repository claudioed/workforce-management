package telemetry

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

// Attribute keys used for log/trace correlation. These are the conventional
// names the OTel Collector's and Loki's trace-to-logs linking expect.
const (
	traceIDKey = "trace_id"
	spanIDKey  = "span_id"
)

// TraceHandler wraps a slog.Handler and stamps trace_id/span_id onto every
// record emitted with a context that carries a valid span context. Records
// logged without an active span are passed through untouched, so nothing
// gains empty correlation fields.
//
// This is what makes a log line joinable to its trace: it requires callers
// to use the *Context variants (logger.InfoContext(ctx, ...)), which the
// HTTP request-logging middleware does.
type TraceHandler struct {
	inner slog.Handler
}

// NewTraceHandler wraps inner so its records carry trace correlation.
func NewTraceHandler(inner slog.Handler) *TraceHandler {
	return &TraceHandler{inner: inner}
}

// Enabled reports whether inner handles records at the given level.
func (h *TraceHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

// Handle appends trace_id/span_id when ctx carries a valid span context,
// then delegates to inner.
func (h *TraceHandler) Handle(ctx context.Context, record slog.Record) error {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		record = record.Clone()
		record.AddAttrs(
			slog.String(traceIDKey, sc.TraceID().String()),
			slog.String(spanIDKey, sc.SpanID().String()),
		)
	}
	return h.inner.Handle(ctx, record)
}

// WithAttrs returns a TraceHandler wrapping inner.WithAttrs, so correlation
// survives logger.With(...).
func (h *TraceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &TraceHandler{inner: h.inner.WithAttrs(attrs)}
}

// WithGroup returns a TraceHandler wrapping inner.WithGroup, so correlation
// survives logger.WithGroup(...).
func (h *TraceHandler) WithGroup(name string) slog.Handler {
	return &TraceHandler{inner: h.inner.WithGroup(name)}
}
