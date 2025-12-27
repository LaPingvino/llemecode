package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/LaPingvino/llemecode/internal/config"
)

// AddCustomToolTool allows the LLM to create new command-line tools
type AddCustomToolTool struct {
	registry *Registry
	config   *config.Config
}

func NewAddCustomToolTool(registry *Registry, cfg *config.Config) *AddCustomToolTool {
	return &AddCustomToolTool{
		registry: registry,
		config:   cfg,
	}
}

func (t *AddCustomToolTool) Name() string {
	return "add_custom_tool"
}

func (t *AddCustomToolTool) Description() string {
	return "Create a new custom command-line tool that can be used in subsequent operations. This allows you to create specialized tools for specific tasks by wrapping shell commands with named parameters."
}

func (t *AddCustomToolTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name": map[string]interface{}{
				"type":        "string",
				"description": "Name of the tool (alphanumeric and underscores only)",
			},
			"description": map[string]interface{}{
				"type":        "string",
				"description": "Description of what the tool does",
			},
			"command": map[string]interface{}{
				"type":        "string",
				"description": "Shell command template with {{param_name}} placeholders for parameters",
			},
			"params": map[string]interface{}{
				"type":        "array",
				"description": "List of parameters the tool accepts",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"name": map[string]interface{}{
							"type":        "string",
							"description": "Parameter name",
						},
						"type": map[string]interface{}{
							"type":        "string",
							"description": "Parameter type (string, number, boolean)",
						},
						"description": map[string]interface{}{
							"type":        "string",
							"description": "Parameter description",
						},
						"required": map[string]interface{}{
							"type":        "boolean",
							"description": "Whether the parameter is required",
						},
					},
				},
			},
		},
		"required": []string{"name", "description", "command"},
	}
}

func (t *AddCustomToolTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	name, ok := args["name"].(string)
	if !ok {
		return "", fmt.Errorf("name must be a string")
	}

	description, ok := args["description"].(string)
	if !ok {
		return "", fmt.Errorf("description must be a string")
	}

	command, ok := args["command"].(string)
	if !ok {
		return "", fmt.Errorf("command must be a string")
	}

	// Parse parameters
	var params []CommandParam
	if paramsData, ok := args["params"].([]interface{}); ok {
		for _, p := range paramsData {
			paramMap, ok := p.(map[string]interface{})
			if !ok {
				continue
			}

			param := CommandParam{
				Name:        getStringField(paramMap, "name"),
				Type:        getStringField(paramMap, "type"),
				Description: getStringField(paramMap, "description"),
				Required:    getBoolField(paramMap, "required"),
			}
			params = append(params, param)
		}
	}

	// Create the custom tool
	customTool := NewCustomCommandTool(name, description, command, params)

	// Register it
	t.registry.Register(customTool)

	// Save to config
	if t.config.CustomTools == nil {
		t.config.CustomTools = []map[string]interface{}{}
	}

	toolData, err := SerializeCustomTool(customTool)
	if err != nil {
		return "", fmt.Errorf("failed to serialize tool: %w", err)
	}

	t.config.CustomTools = append(t.config.CustomTools, toolData)
	if err := t.config.Save(); err != nil {
		return "", fmt.Errorf("failed to save config: %w", err)
	}

	return fmt.Sprintf("✓ Created custom tool '%s'. You can now use it by calling the tool with the defined parameters.", name), nil
}

// RemoveCustomToolTool allows the LLM to remove custom tools
type RemoveCustomToolTool struct {
	registry *Registry
	config   *config.Config
}

func NewRemoveCustomToolTool(registry *Registry, cfg *config.Config) *RemoveCustomToolTool {
	return &RemoveCustomToolTool{
		registry: registry,
		config:   cfg,
	}
}

func (t *RemoveCustomToolTool) Name() string {
	return "remove_custom_tool"
}

func (t *RemoveCustomToolTool) Description() string {
	return "Remove a previously created custom command-line tool."
}

func (t *RemoveCustomToolTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name": map[string]interface{}{
				"type":        "string",
				"description": "Name of the custom tool to remove",
			},
		},
		"required": []string{"name"},
	}
}

func (t *RemoveCustomToolTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	name, ok := args["name"].(string)
	if !ok {
		return "", fmt.Errorf("name must be a string")
	}

	// Remove from registry
	t.registry.Unregister(name)

	// Remove from config
	newCustomTools := []map[string]interface{}{}
	found := false

	for _, toolData := range t.config.CustomTools {
		if toolName, ok := toolData["name"].(string); ok && toolName == name {
			found = true
			continue
		}
		newCustomTools = append(newCustomTools, toolData)
	}

	if !found {
		return "", fmt.Errorf("custom tool '%s' not found", name)
	}

	t.config.CustomTools = newCustomTools
	if err := t.config.Save(); err != nil {
		return "", fmt.Errorf("failed to save config: %w", err)
	}

	return fmt.Sprintf("✓ Removed custom tool '%s'", name), nil
}

// ListCustomToolsTool allows viewing all custom tools
type ListCustomToolsTool struct {
	config *config.Config
}

func NewListCustomToolsTool(cfg *config.Config) *ListCustomToolsTool {
	return &ListCustomToolsTool{config: cfg}
}

func (t *ListCustomToolsTool) Name() string {
	return "list_custom_tools"
}

func (t *ListCustomToolsTool) Description() string {
	return "List all custom command-line tools that have been created."
}

func (t *ListCustomToolsTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}

func (t *ListCustomToolsTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	if len(t.config.CustomTools) == 0 {
		return "No custom tools have been created yet.", nil
	}

	result := "Custom Tools:\n\n"
	for _, toolData := range t.config.CustomTools {
		jsonData, err := json.MarshalIndent(toolData, "", "  ")
		if err != nil {
			result += fmt.Sprintf("(error formatting tool data: %v)\n\n", err)
		} else {
			result += string(jsonData) + "\n\n"
		}
	}

	return result, nil
}
