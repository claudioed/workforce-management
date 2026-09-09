package mcp

import (
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// NewServer builds the MCP server for this bounded context with every tool,
// resource, and workflow prompt registered.
func NewServer(deps Deps) *mcp.Server {
	server := mcp.NewServer(
		&mcp.Implementation{Name: "workforce-management-mcp", Version: "1.0.0"},
		&mcp.ServerOptions{
			Instructions: "Read and act on warehouse workforce staffing: planned-vs-active heads per process path (get_staffing_gap), headcount sizing (propose_path_heads), and assigning a certified associate to a path (assign_labor, read-write). Start with the cover_staffing_gaps prompt.",
		},
	)

	deps.registerTools(server)
	deps.registerResources(server)
	deps.registerPrompts(server)

	return server
}

// Handler returns the Streamable HTTP handler for the MCP server.
func Handler(server *mcp.Server) http.Handler {
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
}
