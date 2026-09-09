package mcp

import (
	"net/http"
	"testing"

	"github.com/claudioed/workforce-management/internal/adapters/inbound/auth"
)

func req(authHeader string) *http.Request {
	r, _ := http.NewRequest(http.MethodPost, "http://x/", nil)
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	return r
}

// The MCP adapter now delegates to internal/adapters/inbound/auth (ADR 0005
// fleet decision). These tests pin the behaviour the MCP surface relies on
// through the MCP package's own names, so a regression in the aliasing or in
// the shared package would surface here.
func TestStaticKeyAuth_Authenticate(t *testing.T) {
	authn := NewStaticKeyAuth(map[string]Scope{
		"read-key":  ScopeRead,
		"write-key": ScopeReadWrite,
		"":          ScopeReadWrite, // empty must be dropped
	})

	tests := []struct {
		name      string
		header    string
		wantScope Scope
		wantOK    bool
	}{
		{"valid read key", "Bearer read-key", ScopeRead, true},
		{"valid write key", "Bearer write-key", ScopeReadWrite, true},
		{"case-insensitive scheme", "bearer read-key", ScopeRead, true},
		{"unknown key", "Bearer nope", "", false},
		{"no header", "", "", false},
		{"wrong scheme", "Basic read-key", "", false},
		{"bearer no token", "Bearer ", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			scope, ok := authn.Authenticate(req(tc.header))
			if ok != tc.wantOK || scope != tc.wantScope {
				t.Fatalf("Authenticate(%q) = (%q, %v), want (%q, %v)", tc.header, scope, ok, tc.wantScope, tc.wantOK)
			}
		})
	}
}

func TestStaticKeyAuth_EmptyKeySetRejectsAll(t *testing.T) {
	authn := NewStaticKeyAuth(map[string]Scope{"": ScopeReadWrite})
	if _, ok := authn.Authenticate(req("Bearer anything")); ok {
		t.Fatal("empty key set must reject all requests")
	}
}

func TestStaticKeyAuth_IsTheSharedFleetType(t *testing.T) {
	// The MCP names must be aliases of the shared package's types, not a
	// second copy: an auth.Authenticator built elsewhere is usable here.
	var _ Authenticator = auth.NewStaticKeyAuth(map[string]auth.Scope{"k": auth.ScopeRead})
	if ScopeRead != auth.ScopeRead || ScopeReadWrite != auth.ScopeReadWrite {
		t.Fatal("MCP scope constants must equal the shared auth scopes")
	}
}

func TestScopeAllows(t *testing.T) {
	tests := []struct {
		granted, required Scope
		want              bool
	}{
		{ScopeReadWrite, ScopeRead, true},
		{ScopeReadWrite, ScopeReadWrite, true},
		{ScopeRead, ScopeRead, true},
		{ScopeRead, ScopeReadWrite, false},
		{"", ScopeRead, false},
	}
	for _, tc := range tests {
		if got := scopeAllows(tc.granted, tc.required); got != tc.want {
			t.Errorf("scopeAllows(%q, %q) = %v, want %v", tc.granted, tc.required, got, tc.want)
		}
	}
}
