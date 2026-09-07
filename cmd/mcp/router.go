package main

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// newRouter wraps the authenticated MCP handler in a small chi router so the
// binary is deployable behind Kubernetes probes:
//
//   - GET /healthz  -> 200 {"status":"ok"}, unauthenticated. Liveness and
//     readiness probes carry no bearer key, so this route must sit OUTSIDE
//     the auth middleware. It reveals nothing but "the process is serving".
//   - /  and /mcp   -> the MCP Streamable HTTP handler (auth enforced inside
//     it). Both paths are served so warehouse-ops-agent's *_MCP_ENDPOINT
//     convention (".../mcp") and the pre-existing root-mounted endpoint keep
//     working without a client-side change.
//
// Every other request still goes through the MCP handler and therefore its
// bearer check — the health route is the only unauthenticated surface.
func newRouter(mcpHandler http.Handler) http.Handler {
	r := chi.NewRouter()
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	r.Mount("/mcp", mcpHandler)
	r.Mount("/", mcpHandler)
	return r
}
