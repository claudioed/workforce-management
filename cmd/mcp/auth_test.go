package main

import (
	"io"
	"log/slog"
	"testing"

	inboundmcp "github.com/claudioed/workforce-management/internal/adapters/inbound/mcp"
)

// authKeys now reads through the shared auth package: API_* keys are the
// primary names and MCP_* the fallbacks (fleet ADR 0005 §3), so the existing
// MCP Secret keeps working and a single Secret can serve both surfaces.
func TestAuthKeys_APIKeysWithMCPFallback(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("no keys", func(t *testing.T) {
		if got := authKeys(logger); len(got) != 0 {
			t.Fatalf("authKeys() = %v, want empty", got)
		}
	})
	t.Run("MCP fallback names", func(t *testing.T) {
		t.Setenv("MCP_READ_KEY", "mcp-r")
		t.Setenv("MCP_READWRITE_KEY", "mcp-rw")
		got := authKeys(logger)
		if got["mcp-r"] != inboundmcp.ScopeRead || got["mcp-rw"] != inboundmcp.ScopeReadWrite {
			t.Fatalf("authKeys() = %v", got)
		}
	})
	t.Run("API names win over MCP names", func(t *testing.T) {
		t.Setenv("MCP_READ_KEY", "mcp-r")
		t.Setenv("API_READ_KEY", "api-r")
		got := authKeys(logger)
		if got["api-r"] != inboundmcp.ScopeRead {
			t.Fatalf("API_READ_KEY must grant read: %v", got)
		}
		if _, ok := got["mcp-r"]; ok {
			t.Fatalf("MCP_READ_KEY must be ignored when API_READ_KEY is set: %v", got)
		}
	})
}
