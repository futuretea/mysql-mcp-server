package mcp

import (
	"context"
	"fmt"
	"net/http"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/rs/zerolog/log"

	"example.invalid/mcp-template-module-placeholder/internal/toolcatalog"
	"example.invalid/mcp-template-module-placeholder/pkg/core/config"
	"example.invalid/mcp-template-module-placeholder/pkg/core/version"
	"example.invalid/mcp-template-module-placeholder/pkg/toolset"
)

// Configuration holds server startup settings.
type Configuration struct {
	*config.StaticConfig
	Toolsets []toolset.Toolset
}

// Server owns the MCP server and registered tools.
type Server struct {
	configuration *Configuration
	server        *server.MCPServer
	enabledTools  []string
}

// NewServer creates and configures an MCP server.
func NewServer(configuration Configuration) (*Server, error) {
	if configuration.StaticConfig == nil {
		return nil, fmt.Errorf("static config is required")
	}

	s := &Server{
		configuration: &configuration,
		server: server.NewMCPServer(
			version.BinaryName,
			version.Version,
			server.WithToolCapabilities(true),
			server.WithLogging(),
		),
	}

	if err := s.registerTools(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Server) registerTools() error {
	filtered, err := toolcatalog.Build(s.configuration.Toolsets, toolset.FilterOptions{
		EnabledTools:    s.configuration.EnabledTools,
		DisabledTools:   s.configuration.DisabledTools,
		EnabledDomains:  s.configuration.EnabledDomains,
		DisabledDomains: s.configuration.DisabledDomains,
	})
	if err != nil {
		return fmt.Errorf("build tool catalog: %w", err)
	}

	for _, tool := range filtered {
		s.registerTool(tool)
	}

	if len(s.enabledTools) == 0 {
		return fmt.Errorf("no tools registered; check enabled_tools / disabled_tools / domain filters")
	}

	log.Info().Int("count", len(s.enabledTools)).Msg("registered MCP tools")
	return nil
}

func (s *Server) registerTool(tool toolset.ServerTool) {
	s.server.AddTool(tool.Tool, newToolHandler(tool))
	s.enabledTools = append(s.enabledTools, tool.Tool.Name)
}

func newToolHandler(tool toolset.ServerTool) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		params := request.GetArguments()
		if params == nil {
			params = map[string]any{}
		}

		result, err := tool.Handler(ctx, params)
		return NewTextResult(result, err), nil
	}
}

// GetEnabledTools returns registered tool names.
func (s *Server) GetEnabledTools() []string {
	return append([]string(nil), s.enabledTools...)
}

// IsHealthy reports whether the server has registered tools.
func (s *Server) IsHealthy() bool {
	return s != nil && s.configuration != nil && len(s.enabledTools) > 0
}

// Close releases server resources.
func (s *Server) Close() {}

// ServeStdio starts the MCP server over stdin/stdout.
func (s *Server) ServeStdio() error {
	return server.ServeStdio(s.server)
}

// ServeSSE creates an SSE MCP HTTP handler.
func (s *Server) ServeSSE(baseURL string, httpServer *http.Server) *server.SSEServer {
	options := []server.SSEOption{
		server.WithHTTPServer(httpServer),
		server.WithAppendQueryToMessageEndpoint(),
	}
	if baseURL != "" {
		options = append(options, server.WithBaseURL(baseURL))
	}
	return server.NewSSEServer(s.server, options...)
}

// ServeStreamableHTTP creates a streamable HTTP MCP handler.
func (s *Server) ServeStreamableHTTP(httpServer *http.Server) *server.StreamableHTTPServer {
	return server.NewStreamableHTTPServer(
		s.server,
		server.WithStreamableHTTPServer(httpServer),
		server.WithStateLess(true),
	)
}

// NewTextResult creates a standard MCP text result.
func NewTextResult(content string, err error) *mcp.CallToolResult {
	text := content
	isError := false
	if err != nil {
		text = err.Error()
		isError = true
	}
	return &mcp.CallToolResult{
		IsError: isError,
		Content: []mcp.Content{mcp.TextContent{Type: "text", Text: text}},
	}
}
