package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

// spanContextWithIDs builds a valid, sampled SpanContext without needing a
// real TracerProvider, so this handler test stays hermetic.
func spanContextWithIDs(t *testing.T, traceID, spanID string) trace.SpanContext {
	t.Helper()
	tid, err := trace.TraceIDFromHex(traceID)
	if err != nil {
		t.Fatalf("parse trace id: %v", err)
	}
	sid, err := trace.SpanIDFromHex(spanID)
	if err != nil {
		t.Fatalf("parse span id: %v", err)
	}
	return trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    tid,
		SpanID:     sid,
		TraceFlags: trace.FlagsSampled,
	})
}

func logAndDecode(t *testing.T, ctx context.Context, build func(*slog.Logger) *slog.Logger) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	logger := slog.New(NewTraceHandler(slog.NewJSONHandler(&buf, nil)))
	if build != nil {
		logger = build(logger)
	}
	logger.InfoContext(ctx, "hello", "key", "value")

	var record map[string]any
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("decode log record %q: %v", buf.String(), err)
	}
	return record
}

func TestTraceHandlerAttachesTraceAndSpanIDs(t *testing.T) {
	const (
		traceID = "4bf92f3577b34da6a3ce929d0e0e4736"
		spanID  = "00f067aa0ba902b7"
	)
	ctx := trace.ContextWithSpanContext(context.Background(), spanContextWithIDs(t, traceID, spanID))

	tests := []struct {
		name  string
		build func(*slog.Logger) *slog.Logger
	}{
		{name: "plain logger", build: nil},
		{name: "logger.With survives the wrapper", build: func(l *slog.Logger) *slog.Logger {
			return l.With("component", "test")
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			record := logAndDecode(t, ctx, tc.build)
			if record["trace_id"] != traceID {
				t.Errorf("trace_id = %v, want %q", record["trace_id"], traceID)
			}
			if record["span_id"] != spanID {
				t.Errorf("span_id = %v, want %q", record["span_id"], spanID)
			}
		})
	}
}

func TestTraceHandlerOmitsIDsWithoutAnActiveSpan(t *testing.T) {
	record := logAndDecode(t, context.Background(), nil)

	if _, ok := record["trace_id"]; ok {
		t.Errorf("expected no trace_id without an active span, got %v", record["trace_id"])
	}
	if _, ok := record["span_id"]; ok {
		t.Errorf("expected no span_id without an active span, got %v", record["span_id"])
	}
	if record["msg"] != "hello" || record["key"] != "value" {
		t.Errorf("wrapper altered the record: %v", record)
	}
}

func TestTraceHandlerDelegatesEnabledAndGroup(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})
	h := NewTraceHandler(inner)

	if h.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("Enabled(Info) = true, want false — the wrapper must delegate the inner handler's level")
	}
	if !h.Enabled(context.Background(), slog.LevelError) {
		t.Error("Enabled(Error) = false, want true")
	}

	if _, ok := h.WithGroup("g").(*TraceHandler); !ok {
		t.Error("WithGroup must return a TraceHandler, or correlation is lost after grouping")
	}
	if _, ok := h.WithAttrs([]slog.Attr{slog.String("a", "b")}).(*TraceHandler); !ok {
		t.Error("WithAttrs must return a TraceHandler, or correlation is lost after logger.With")
	}
}
