package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// CustomCommandTool represents a user-defined command-line tool
type CustomCommandTool struct {
	name        string
	description string
	command     string // Template command with {{param}} placeholders
	params      []CommandParam
}

type CommandParam struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

func NewCustomCommandTool(name, description, command string, params []CommandParam) *CustomCommandTool {
	return &CustomCommandTool{
		name:        name,
		description: description,
		command:     command,
		params:      params,
	}
}

func (t *CustomCommandTool) Name() string {
	return t.name
}

func (t *CustomCommandTool) Description() string {
	return t.description
}

func (t *CustomCommandTool) Parameters() map[string]interface{} {
	properties := make(map[string]interface{})
	required := []string{}

	for _, param := range t.params {
		properties[param.Name] = map[string]interface{}{
			"type":        param.Type,
			"description": param.Description,
		}
		if param.Required {
			required = append(required, param.Name)
		}
	}

	schema := map[string]interface{}{
		"type":       "object",
		"properties": properties,
	}

	if len(required) > 0 {
		schema["required"] = required
	}

	return schema
}

func (t *CustomCommandTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	// Build command by replacing placeholders
	cmd := t.command

	for _, param := range t.params {
		placeholder := fmt.Sprintf("{{%s}}", param.Name)
		value, ok := args[param.Name]
		if !ok {
			if param.Required {
				return "", fmt.Errorf("missing required parameter: %s", param.Name)
			}
			value = ""
		}

		// Convert value to string
		var valueStr string
		switch v := value.(type) {
		case string:
			valueStr = v
		case float64:
			valueStr = fmt.Sprintf("%v", v)
		case bool:
			valueStr = fmt.Sprintf("%v", v)
		default:
			jsonBytes, err := json.Marshal(v)
			if err != nil {
				return "", fmt.Errorf("failed to convert parameter %s: %w", param.Name, err)
			}
			valueStr = string(jsonBytes)
		}

		cmd = strings.ReplaceAll(cmd, placeholder, valueStr)
	}

	// Execute command
	execCmd := exec.CommandContext(ctx, "sh", "-c", cmd)
	output, err := execCmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("command failed: %w\nOutput: %s", err, string(output))
	}

	return string(output), nil
}

// SerializeCustomTool converts a custom tool to JSON for storage
func SerializeCustomTool(tool *CustomCommandTool) (map[string]interface{}, error) {
	return map[string]interface{}{
		"name":        tool.name,
		"description": tool.description,
		"command":     tool.command,
		"params":      tool.params,
	}, nil
}

// DeserializeCustomTool creates a custom tool from JSON
func DeserializeCustomTool(data map[string]interface{}) (*CustomCommandTool, error) {
	name, ok := data["name"].(string)
	if !ok {
		return nil, fmt.Errorf("missing or invalid 'name' field")
	}

	description, ok := data["description"].(string)
	if !ok {
		return nil, fmt.Errorf("missing or invalid 'description' field")
	}

	command, ok := data["command"].(string)
	if !ok {
		return nil, fmt.Errorf("missing or invalid 'command' field")
	}

	var params []CommandParam
	if paramsData, ok := data["params"].([]interface{}); ok {
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

	return NewCustomCommandTool(name, description, command, params), nil
}

func getStringField(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getBoolField(m map[string]interface{}, key string) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return false
}
