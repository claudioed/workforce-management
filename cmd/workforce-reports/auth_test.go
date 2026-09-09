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
	if authn.HasKeys() || mode != auth.ModeOff {
		t.Fatalf("got keys=%v mode=%q, want no keys / off", authn.HasKeys(), mode)
	}
}

func TestConfigureAuth_DefaultsEnforceWithKeysAndHonoursAUTH_MODE(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	authn, mode := configureAuth(envFrom(map[string]string{"API_READ_KEY": "r"}), logger)
	if !authn.HasKeys() || mode != auth.ModeEnforce {
		t.Fatalf("got keys=%v mode=%q, want keys / enforce", authn.HasKeys(), mode)
	}
	_, mode = configureAuth(envFrom(map[string]string{"MCP_READ_KEY": "r", "AUTH_MODE": "log"}), logger)
	if mode != auth.ModeLog {
		t.Fatalf("mode = %q, want log", mode)
	}
}
