package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/LaPingvino/llemecode/internal/logger"
	"github.com/LaPingvino/llemecode/internal/tools"
)

// TestToolCommand allows testing tool invocation directly
type TestToolCommand struct {
	toolRegistry *tools.Registry
}

func NewTestToolCommand(toolRegistry *tools.Registry) *TestToolCommand {
	return &TestToolCommand{toolRegistry: toolRegistry}
}

func (c *TestToolCommand) Name() string {
	return "test-tool"
}

func (c *TestToolCommand) Description() string {
	return "Test a tool directly (usage: /test-tool <tool-name> <json-args>)"
}

func (c *TestToolCommand) Execute(ctx context.Context, args []string, m *chatModel) (string, error) {
	if len(args) < 1 {
		return "", fmt.Errorf("usage: /test-tool <tool-name> [json-args]\n\nExample: /test-tool read_file {\"file_path\": \"/etc/hosts\"}")
	}

	toolName := args[0]

	// Parse args if provided
	var toolArgs map[string]interface{}
	if len(args) > 1 {
		argsJSON := strings.Join(args[1:], " ")
		if err := json.Unmarshal([]byte(argsJSON), &toolArgs); err != nil {
			return "", fmt.Errorf("invalid JSON args: %w\n\nExample: /test-tool read_file {\"file_path\": \"/etc/hosts\"}", err)
		}
	} else {
		toolArgs = make(map[string]interface{})
	}

	logger.Log("TestToolCommand: Testing tool %q with args: %v", toolName, toolArgs)

	// Execute the tool
	result, err := m.agent.GetToolRegistry().Execute(ctx, toolName, toolArgs)

	if err != nil {
		logger.Log("TestToolCommand: Tool %q returned error: %v", toolName, err)
		return "", fmt.Errorf("tool execution failed: %w", err)
	}

	logger.Log("TestToolCommand: Tool %q returned result (length %d)", toolName, len(result))

	return fmt.Sprintf("✓ Tool '%s' executed successfully!\n\nResult:\n```\n%s\n```", toolName, result), nil
}
