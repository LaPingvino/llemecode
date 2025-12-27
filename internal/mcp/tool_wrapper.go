package mcp

import (
	"context"
	"fmt"

	"github.com/LaPingvino/llemecode/internal/tools"
)

// MCPToolWrapper wraps an MCP tool to make it compatible with Llemecode's tool interface
type MCPToolWrapper struct {
	client      *MCPClient
	mcpTool     MCPTool
	fullName    string
	description string
}

// NewMCPToolWrapper creates a new wrapper for an MCP tool
func NewMCPToolWrapper(client *MCPClient, mcpTool MCPTool) *MCPToolWrapper {
	// Prefix tool name with server name to avoid conflicts
	fullName := fmt.Sprintf("mcp_%s_%s", client.ServerName(), mcpTool.Name)

	// Add server info to description
	description := fmt.Sprintf("[MCP: %s] %s", client.ServerName(), mcpTool.Description)

	return &MCPToolWrapper{
		client:      client,
		mcpTool:     mcpTool,
		fullName:    fullName,
		description: description,
	}
}

func (w *MCPToolWrapper) Name() string {
	return w.fullName
}

func (w *MCPToolWrapper) Description() string {
	return w.description
}

func (w *MCPToolWrapper) Parameters() map[string]interface{} {
	// Return the MCP tool's input schema directly
	// It should already be in JSON Schema format
	return w.mcpTool.InputSchema
}

func (w *MCPToolWrapper) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	// Call the MCP server with the original tool name (not prefixed)
	return w.client.CallTool(ctx, w.mcpTool.Name, args)
}

// MCPToolRegistry manages multiple MCP servers and their tools
type MCPToolRegistry struct {
	clients map[string]*MCPClient
}

func NewMCPToolRegistry() *MCPToolRegistry {
	return &MCPToolRegistry{
		clients: make(map[string]*MCPClient),
	}
}

// AddServer adds an MCP server
func (r *MCPToolRegistry) AddServer(ctx context.Context, serverName, command string, args []string) error {
	client := NewMCPClient(serverName, command, args)

	if err := client.Start(ctx); err != nil {
		return fmt.Errorf("failed to start MCP server %s: %w", serverName, err)
	}

	r.clients[serverName] = client
	return nil
}

// GetTools returns all tools from all MCP servers as Llemecode tools
func (r *MCPToolRegistry) GetTools() []tools.Tool {
	var allTools []tools.Tool

	for _, client := range r.clients {
		mcpTools := client.GetTools()
		for _, mcpTool := range mcpTools {
			wrapper := NewMCPToolWrapper(client, mcpTool)
			allTools = append(allTools, wrapper)
		}
	}

	return allTools
}

// Close closes all MCP server connections
func (r *MCPToolRegistry) Close() error {
	for _, client := range r.clients {
		client.Close()
	}
	return nil
}

// GetServerNames returns the names of all registered servers
func (r *MCPToolRegistry) GetServerNames() []string {
	names := make([]string, 0, len(r.clients))
	for name := range r.clients {
		names = append(names, name)
	}
	return names
}
