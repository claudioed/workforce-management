package mcp

import (
	"github.com/claudioed/workforce-management/internal/adapters/inbound/auth"
)

// The MCP adapter's identity model is the fleet-standard one shared with the
// REST surface (warehouse-ops-agent ADR 0005, this repo's ADR-0017): static
// bearer keys mapped to read / read-write scopes behind an Authenticator seam
// (ADR-0008). The implementation lives in internal/adapters/inbound/auth so a
// repository carries exactly ONE copy serving both surfaces; these aliases
// keep the MCP package's public names (used by cmd/mcp and the tests) stable.
type (
	// Scope is a coarse authorization class carried by an API key.
	Scope = auth.Scope
	// Authenticator validates a request's bearer credential and reports the
	// scope it grants.
	Authenticator = auth.Authenticator
	// StaticKeyAuth authenticates against a fixed set of bearer keys.
	StaticKeyAuth = auth.StaticKeyAuth
)

const (
	ScopeRead      = auth.ScopeRead
	ScopeReadWrite = auth.ScopeReadWrite
)

// NewStaticKeyAuth builds a StaticKeyAuth from token->scope pairs; see
// auth.NewStaticKeyAuth.
func NewStaticKeyAuth(keys map[string]Scope) *StaticKeyAuth {
	return auth.NewStaticKeyAuth(keys)
}

// scopeAllows reports whether a granted scope may call a tool requiring the
// given minimum scope; delegates to the shared auth.Allows.
func scopeAllows(granted, required Scope) bool {
	return auth.Allows(granted, required)
}
