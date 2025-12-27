package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/LaPingvino/llemecode/internal/config"
	"github.com/LaPingvino/llemecode/internal/tools"
)

// DisableToolCommand
type DisableToolCommand struct {
	cfg          *config.Config
	toolRegistry *tools.Registry
}

func NewDisableToolCommand(cfg *config.Config, toolRegistry *tools.Registry) *DisableToolCommand {
	return &DisableToolCommand{cfg: cfg, toolRegistry: toolRegistry}
}

func (c *DisableToolCommand) Name() string {
	return "disabletool"
}

func (c *DisableToolCommand) Description() string {
	return "Disable a tool (usage: /disabletool <tool-name> [--permanent])"
}

func (c *DisableToolCommand) Execute(ctx context.Context, args []string, m *chatModel) (string, error) {
	if len(args) == 0 {
		return "Usage: /disabletool <tool-name> [--permanent]\n\nAdd --permanent to save to config file.", nil
	}

	toolName := args[0]
	permanent := false

	// Check for --permanent flag
	if len(args) > 1 && args[1] == "--permanent" {
		permanent = true
	}

	// Verify tool exists
	if _, ok := c.toolRegistry.Get(toolName); !ok {
		return "", fmt.Errorf("tool '%s' not found. Use /tools to see available tools", toolName)
	}

	// Check if already disabled
	if m.sessionDisabledTools[toolName] {
		return fmt.Sprintf("Tool '%s' is already disabled in this session", toolName), nil
	}

	if permanent {
		// Check if already disabled in config
		for _, disabled := range c.cfg.DisabledTools {
			if disabled == toolName {
				return fmt.Sprintf("Tool '%s' is already permanently disabled", toolName), nil
			}
		}

		// Add to config
		c.cfg.DisabledTools = append(c.cfg.DisabledTools, toolName)
		if err := c.cfg.Save(); err != nil {
			return "", fmt.Errorf("failed to save config: %w", err)
		}

		// Also disable in session
		m.sessionDisabledTools[toolName] = true
		m.updateAgentDisabledTools(c.cfg)

		return fmt.Sprintf("✓ Tool '%s' disabled permanently and saved to config", toolName), nil
	}

	// Session-only disable
	m.sessionDisabledTools[toolName] = true
	m.updateAgentDisabledTools(c.cfg)
	return fmt.Sprintf("✓ Tool '%s' disabled for this session only", toolName), nil
}

// EnableToolCommand
type EnableToolCommand struct {
	cfg          *config.Config
	toolRegistry *tools.Registry
}

func NewEnableToolCommand(cfg *config.Config, toolRegistry *tools.Registry) *EnableToolCommand {
	return &EnableToolCommand{cfg: cfg, toolRegistry: toolRegistry}
}

func (c *EnableToolCommand) Name() string {
	return "enabletool"
}

func (c *EnableToolCommand) Description() string {
	return "Enable a disabled tool (usage: /enabletool <tool-name> [--permanent])"
}

func (c *EnableToolCommand) Execute(ctx context.Context, args []string, m *chatModel) (string, error) {
	if len(args) == 0 {
		return "Usage: /enabletool <tool-name> [--permanent]\n\nAdd --permanent to remove from config file.", nil
	}

	toolName := args[0]
	permanent := false

	// Check for --permanent flag
	if len(args) > 1 && args[1] == "--permanent" {
		permanent = true
	}

	// Verify tool exists
	if _, ok := c.toolRegistry.Get(toolName); !ok {
		return "", fmt.Errorf("tool '%s' not found. Use /tools to see available tools", toolName)
	}

	// Check session disabled
	wasSessionDisabled := m.sessionDisabledTools[toolName]
	delete(m.sessionDisabledTools, toolName)

	if permanent {
		// Remove from config
		newDisabled := []string{}
		found := false
		for _, disabled := range c.cfg.DisabledTools {
			if disabled != toolName {
				newDisabled = append(newDisabled, disabled)
			} else {
				found = true
			}
		}

		if !found && !wasSessionDisabled {
			return fmt.Sprintf("Tool '%s' was not disabled", toolName), nil
		}

		c.cfg.DisabledTools = newDisabled
		if err := c.cfg.Save(); err != nil {
			return "", fmt.Errorf("failed to save config: %w", err)
		}

		m.updateAgentDisabledTools(c.cfg)
		return fmt.Sprintf("✓ Tool '%s' enabled permanently and removed from config", toolName), nil
	}

	// Session-only enable
	if !wasSessionDisabled {
		// Check if disabled in config
		disabledInConfig := false
		for _, disabled := range c.cfg.DisabledTools {
			if disabled == toolName {
				disabledInConfig = true
				break
			}
		}

		if disabledInConfig {
			return fmt.Sprintf("Tool '%s' is disabled in config. Use --permanent to enable it.", toolName), nil
		}

		return fmt.Sprintf("Tool '%s' was not disabled", toolName), nil
	}

	m.updateAgentDisabledTools(c.cfg)
	return fmt.Sprintf("✓ Tool '%s' enabled for this session only", toolName), nil
}

// ListDisabledToolsCommand
type ListDisabledToolsCommand struct {
	cfg *config.Config
}

func NewListDisabledToolsCommand(cfg *config.Config) *ListDisabledToolsCommand {
	return &ListDisabledToolsCommand{cfg: cfg}
}

func (c *ListDisabledToolsCommand) Name() string {
	return "disabledtools"
}

func (c *ListDisabledToolsCommand) Description() string {
	return "List all disabled tools"
}

func (c *ListDisabledToolsCommand) Execute(ctx context.Context, args []string, m *chatModel) (string, error) {
	var sb strings.Builder
	sb.WriteString("Disabled tools:\n\n")

	// Config-level disabled
	if len(c.cfg.DisabledTools) > 0 {
		sb.WriteString("**Permanently disabled (in config):**\n")
		for _, toolName := range c.cfg.DisabledTools {
			sb.WriteString(fmt.Sprintf("• %s\n", toolName))
		}
		sb.WriteString("\n")
	}

	// Session-level disabled
	sessionDisabled := []string{}
	for toolName := range m.sessionDisabledTools {
		// Skip if also in config (already listed above)
		inConfig := false
		for _, disabled := range c.cfg.DisabledTools {
			if disabled == toolName {
				inConfig = true
				break
			}
		}
		if !inConfig {
			sessionDisabled = append(sessionDisabled, toolName)
		}
	}

	if len(sessionDisabled) > 0 {
		sb.WriteString("**Session-only disabled:**\n")
		for _, toolName := range sessionDisabled {
			sb.WriteString(fmt.Sprintf("• %s\n", toolName))
		}
		sb.WriteString("\n")
	}

	if len(c.cfg.DisabledTools) == 0 && len(sessionDisabled) == 0 {
		sb.WriteString("No tools are currently disabled.\n\n")
	}

	sb.WriteString("Use `/enabletool <name>` to enable a tool.\n")
	sb.WriteString("Use `/disabletool <name> --permanent` to disable permanently.")

	return sb.String(), nil
}
