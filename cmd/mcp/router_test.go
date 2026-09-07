package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	inboundmcp "github.com/claudioed/workforce-management/internal/adapters/inbound/mcp"
)

// newTestRouter builds the router exactly as run() does: the real MCP server
// (in-memory deps are fine — no tool is invoked here) behind the real static
// key auth, so the tests exercise the actual auth boundary, not a stub.
func newTestRouter(t *testing.T) http.Handler {
	t.Helper()
	server := inboundmcp.NewServer(inboundmcp.Deps{})
	auth := inboundmcp.NewStaticKeyAuth(map[string]inboundmcp.Scope{"read-key": inboundmcp.ScopeRead})
	return newRouter(inboundmcp.Handler(server, auth))
}

func TestRouter_HealthzIsUnauthenticated(t *testing.T) {
	h := newTestRouter(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /healthz without a bearer key: got %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != `{"status":"ok"}` {
		t.Fatalf("GET /healthz body = %q, want {\"status\":\"ok\"}", got)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("GET /healthz Content-Type = %q, want application/json", ct)
	}
	if rec.Header().Get("WWW-Authenticate") != "" {
		t.Fatal("GET /healthz must not go through the auth middleware")
	}
}

func TestRouter_MCPPathsRequireBearer(t *testing.T) {
	h := newTestRouter(t)
	for _, path := range []string{"/", "/mcp"} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
			req.Header.Set("Content-Type", "application/json")
			h.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("POST %s without bearer: got %d, want 401", path, rec.Code)
			}
			if got := rec.Header().Get("WWW-Authenticate"); !strings.HasPrefix(got, "Bearer ") {
				t.Fatalf("POST %s: WWW-Authenticate = %q, want a Bearer challenge (proves the MCP auth handler answered)", path, got)
			}
		})
	}
}

func TestRouter_MCPPathsReachStreamableHandlerWithBearer(t *testing.T) {
	h := newTestRouter(t)
	for _, path := range []string{"/", "/mcp"} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			// initialize is the first Streamable HTTP request a client sends;
			// a non-401 JSON-RPC reply proves the request passed auth and
			// reached the SDK handler at this mount point.
			body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")
			req.Header.Set("Authorization", "Bearer read-key")
			h.ServeHTTP(rec, req)

			if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusNotFound {
				t.Fatalf("POST %s with bearer: got %d, want the MCP handler to answer", path, rec.Code)
			}
			raw, _ := io.ReadAll(rec.Body)
			if !strings.Contains(string(raw), `"jsonrpc"`) {
				t.Fatalf("POST %s with bearer: body %q does not look like a JSON-RPC reply", path, string(raw))
			}
		})
	}
}

func TestRouter_UnknownPathStillGuardedByAuth(t *testing.T) {
	h := newTestRouter(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/anything-else", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET /anything-else: got %d, want 401 — /healthz must be the only unauthenticated surface", rec.Code)
	}
}
