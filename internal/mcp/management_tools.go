package mcp

import (
	"context"
	"fmt"

	"github.com/LaPingvino/llemecode/internal/config"
	"github.com/LaPingvino/llemecode/internal/tools"
)

// AddMCPServerTool allows the LLM to add MCP servers
type AddMCPServerTool struct {
	registry *MCPToolRegistry
	config   *config.Config
	toolReg  *tools.Registry
	ctx      context.Context
}

func NewAddMCPServerTool(registry *MCPToolRegistry, cfg *config.Config, toolReg *tools.Registry, ctx context.Context) *AddMCPServerTool {
	return &AddMCPServerTool{
		registry: registry,
		config:   cfg,
		toolReg:  toolReg,
		ctx:      ctx,
	}
}

func (t *AddMCPServerTool) Name() string {
	return "add_mcp_server"
}

func (t *AddMCPServerTool) Description() string {
	return "Add an MCP (Model Context Protocol) server to access external tools. MCP servers expose tools that can be used for various tasks like filesystem operations, web browsing, database access, etc."
}

func (t *AddMCPServerTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name": map[string]interface{}{
				"type":        "string",
				"description": "Unique name for this MCP server",
			},
			"command": map[string]interface{}{
				"type":        "string",
				"description": "Command to start the MCP server (e.g., 'npx', 'python', '/path/to/server')",
			},
			"args": map[string]interface{}{
				"type":        "array",
				"description": "Arguments to pass to the command",
				"items": map[string]interface{}{
					"type": "string",
				},
			},
			"permanent": map[string]interface{}{
				"type":        "boolean",
				"description": "Save to config for persistence across sessions (default: false)",
			},
		},
		"required": []string{"name", "command"},
	}
}

func (t *AddMCPServerTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	name, ok := args["name"].(string)
	if !ok {
		return "", fmt.Errorf("name must be a string")
	}

	command, ok := args["command"].(string)
	if !ok {
		return "", fmt.Errorf("command must be a string")
	}

	var cmdArgs []string
	if argsData, ok := args["args"].([]interface{}); ok {
		for _, arg := range argsData {
			if argStr, ok := arg.(string); ok {
				cmdArgs = append(cmdArgs, argStr)
			}
		}
	}

	permanent := false
	if p, ok := args["permanent"].(bool); ok {
		permanent = p
	}

	// Add the server
	if err := t.registry.AddServer(t.ctx, name, command, cmdArgs); err != nil {
		return "", fmt.Errorf("failed to add MCP server: %w", err)
	}

	// Register the tools from this server
	mcpTools := t.registry.GetTools()
	toolsAdded := 0
	for _, mcpTool := range mcpTools {
		// Only register tools from the newly added server
		if mcpTool.Name()[:len("mcp_"+name)] == "mcp_"+name {
			// Use safe permission for MCP tools initially
			t.toolReg.Register(mcpTool)
			toolsAdded++
		}
	}

	result := fmt.Sprintf("✓ Added MCP server '%s' with %d tools\n", name, toolsAdded)

	// Save to config if permanent
	if permanent {
		t.config.MCPServers = append(t.config.MCPServers, config.MCPServerConfig{
			Name:    name,
			Command: command,
			Args:    cmdArgs,
			Enabled: true,
		})

		if err := t.config.Save(); err != nil {
			return "", fmt.Errorf("tools added but failed to save config: %w", err)
		}

		result += "Saved to config for future sessions.\n"
	} else {
		result += "Server active for this session only.\n"
	}

	return result, nil
}

// RemoveMCPServerTool allows the LLM to remove MCP servers
type RemoveMCPServerTool struct {
	config *config.Config
}

func NewRemoveMCPServerTool(cfg *config.Config) *RemoveMCPServerTool {
	return &RemoveMCPServerTool{config: cfg}
}

func (t *RemoveMCPServerTool) Name() string {
	return "remove_mcp_server"
}

func (t *RemoveMCPServerTool) Description() string {
	return "Remove an MCP server from the configuration. Note: This only removes it from the config; tools from this server will remain available until restart."
}

func (t *RemoveMCPServerTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name": map[string]interface{}{
				"type":        "string",
				"description": "Name of the MCP server to remove",
			},
		},
		"required": []string{"name"},
	}
}

func (t *RemoveMCPServerTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	name, ok := args["name"].(string)
	if !ok {
		return "", fmt.Errorf("name must be a string")
	}

	// Remove from config
	newServers := []config.MCPServerConfig{}
	found := false

	for _, server := range t.config.MCPServers {
		if server.Name != name {
			newServers = append(newServers, server)
		} else {
			found = true
		}
	}

	if !found {
		return "", fmt.Errorf("MCP server '%s' not found in config", name)
	}

	t.config.MCPServers = newServers
	if err := t.config.Save(); err != nil {
		return "", fmt.Errorf("failed to save config: %w", err)
	}

	return fmt.Sprintf("✓ Removed MCP server '%s' from config\nNote: Restart required to unload tools", name), nil
}

// ListMCPServersTool shows all configured MCP servers
type ListMCPServersTool struct {
	config   *config.Config
	registry *MCPToolRegistry
}

func NewListMCPServersTool(cfg *config.Config, registry *MCPToolRegistry) *ListMCPServersTool {
	return &ListMCPServersTool{config: cfg, registry: registry}
}

func (t *ListMCPServersTool) Name() string {
	return "list_mcp_servers"
}

func (t *ListMCPServersTool) Description() string {
	return "List all configured MCP servers and their status."
}

func (t *ListMCPServersTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}

func (t *ListMCPServersTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	if len(t.config.MCPServers) == 0 {
		return "No MCP servers configured.\n\nYou can add one with add_mcp_server.", nil
	}

	result := "MCP Servers:\n\n"

	activeServers := t.registry.GetServerNames()
	activeMap := make(map[string]bool)
	for _, name := range activeServers {
		activeMap[name] = true
	}

	for _, server := range t.config.MCPServers {
		status := "❌ Inactive"
		if activeMap[server.Name] {
			status = "✓ Active"
		}

		result += fmt.Sprintf("%s %s\n", status, server.Name)
		result += fmt.Sprintf("  Command: %s %v\n", server.Command, server.Args)
		result += fmt.Sprintf("  Enabled: %v\n\n", server.Enabled)
	}

	result += fmt.Sprintf("Total configured: %d\n", len(t.config.MCPServers))
	result += fmt.Sprintf("Currently active: %d\n", len(activeServers))

	return result, nil
}
