package tools

import (
	"context"
	"fmt"
)

type BashTool struct {
	executor CommandExecutor
}

// CommandExecutor is an interface for executing commands
// This allows different execution strategies (direct, interactive, etc.)
type CommandExecutor interface {
	Execute(ctx context.Context, command string) (output string, exitCode int, err error)
}

func NewBashTool() *BashTool {
	return &BashTool{}
}

// SetExecutor sets the command executor
func (t *BashTool) SetExecutor(executor CommandExecutor) {
	t.executor = executor
}

func (t *BashTool) Name() string {
	return "run_command"
}

func (t *BashTool) Description() string {
	return "Execute a shell command in an interactive window with real-time output, scrolling, and input support (Ctrl+F for passwords/prompts). Use this to run system commands, scripts, or CLI tools."
}

func (t *BashTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"command": map[string]interface{}{
				"type":        "string",
				"description": "The shell command to execute",
			},
		},
		"required": []string{"command"},
	}
}

func (t *BashTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	command, ok := args["command"].(string)
	if !ok {
		return "", fmt.Errorf("command must be a string")
	}

	if t.executor == nil {
		return "", fmt.Errorf("no command executor configured")
	}

	output, exitCode, err := t.executor.Execute(ctx, command)

	if err != nil {
		return fmt.Sprintf("%s\n\nExit code: %d\nError: %v", output, exitCode, err), nil
	}

	return fmt.Sprintf("%s\n\nExit code: %d", output, exitCode), nil
}
