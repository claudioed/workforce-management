package main

import (
	"io"
	"log/slog"
	"testing"

	"github.com/claudioed/workforce-management/internal/adapters/inbound/auth"
)

func envFrom(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestConfigureAuth_DefaultsOffWithoutKeys(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	authn, mode := configureAuth(envFrom(nil), logger)
	if authn.HasKeys() {
		t.Fatal("expected no keys")
	}
	if mode != auth.ModeOff {
		t.Fatalf("mode = %q, want off", mode)
	}
}

func TestConfigureAuth_DefaultsEnforceWithKeys(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	authn, mode := configureAuth(envFrom(map[string]string{"API_READ_KEY": "r"}), logger)
	if !authn.HasKeys() {
		t.Fatal("expected a key")
	}
	if mode != auth.ModeEnforce {
		t.Fatalf("mode = %q, want enforce", mode)
	}
}

func TestConfigureAuth_ExplicitModeAndMCPFallback(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	authn, mode := configureAuth(envFrom(map[string]string{"MCP_READWRITE_KEY": "rw", "AUTH_MODE": "log"}), logger)
	if !authn.HasKeys() {
		t.Fatal("MCP_READWRITE_KEY must be accepted as a fallback")
	}
	if mode != auth.ModeLog {
		t.Fatalf("mode = %q, want log", mode)
	}
	_, mode = configureAuth(envFrom(map[string]string{"API_READ_KEY": "r", "AUTH_MODE": "off"}), logger)
	if mode != auth.ModeOff {
		t.Fatalf("mode = %q, want off (explicit AUTH_MODE=off wins over keys)", mode)
	}
}
