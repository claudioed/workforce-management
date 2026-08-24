package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/claudioed/workforce-management/internal/domain/shared"
)

// registerResources adds the scoped read-model resource. Per the charter,
// resources are bounded-context contracts tied to a decision, not bulk dumps:
// the staffing-gap resource answers "how does one path stand against its
// committed plan?" for a specific building/shift/path, backed by the same
// GetStaffingGap read model the tool uses.
//
// The URI is templated (staffing://{buildingId}/{shiftId}/{pathId}/gap); the
// concrete building/shift/path are read from the request URI at call time.
func (d Deps) registerResources(server *mcp.Server, scopeOf func(context.Context) Scope) {
	const template = "staffing://{buildingId}/{shiftId}/{pathId}/gap"
	server.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: template,
		Name:        "staffing gap",
		Description: "Planned-vs-active heads and understaffed flag for one process path within a building's committed shift plan.",
		MIMEType:    "application/json",
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		if !scopeAllows(scopeOf(ctx), ScopeRead) {
			return nil, fmt.Errorf("resource requires read scope")
		}
		buildingId, shiftId, pathId, err := parseStaffingURI(req.Params.URI)
		if err != nil {
			return nil, err
		}
		gap, err := d.GetStaffingGap.Execute(ctx, buildingId, shiftId, shared.PathId(pathId))
		if err != nil {
			return nil, err
		}
		body, err := json.Marshal(toStaffingGap(buildingId, shiftId, gap))
		if err != nil {
			return nil, err
		}
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{{
				URI:      req.Params.URI,
				MIMEType: "application/json",
				Text:     string(body),
			}},
		}, nil
	})
}

// parseStaffingURI extracts buildingId, shiftId and pathId from a
// staffing://{buildingId}/{shiftId}/{pathId}/gap URI. The three path segments
// are model-supplied and therefore untrusted; a malformed URI or an empty
// segment is rejected rather than silently defaulted.
func parseStaffingURI(uri string) (buildingId, shiftId, pathId string, err error) {
	const scheme = "staffing://"
	rest, ok := trimPrefix(uri, scheme)
	if !ok {
		return "", "", "", fmt.Errorf("resource uri %q must start with %q", uri, scheme)
	}
	parts := splitSlash(rest)
	if len(parts) != 4 || parts[3] != "gap" {
		return "", "", "", fmt.Errorf("resource uri %q must be staffing://{buildingId}/{shiftId}/{pathId}/gap", uri)
	}
	buildingId, shiftId, pathId = parts[0], parts[1], parts[2]
	if buildingId == "" || shiftId == "" || pathId == "" {
		return "", "", "", fmt.Errorf("resource uri %q has an empty buildingId, shiftId or pathId segment", uri)
	}
	return buildingId, shiftId, pathId, nil
}

func trimPrefix(s, prefix string) (string, bool) {
	if len(s) < len(prefix) || s[:len(prefix)] != prefix {
		return "", false
	}
	return s[len(prefix):], true
}

func splitSlash(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}
