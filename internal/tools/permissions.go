package tools

import (
	"context"
	"fmt"
)

// PermissionLevel defines how dangerous a tool operation is
type PermissionLevel int

const (
	PermissionSafe    PermissionLevel = iota
	PermissionRead                    // Read-only operations
	PermissionWrite                   // File modifications
	PermissionExecute                 // Code execution
	PermissionNetwork                 // Network access
)

// PermissionChecker handles user approval for tool operations
type PermissionChecker interface {
	// RequestPermission asks the user for approval
	// Returns true if approved, false if denied
	RequestPermission(ctx context.Context, tool string, level PermissionLevel, details string) (bool, error)
}

// PermissionConfig defines what requires approval
type PermissionConfig struct {
	// Auto-approve safe operations
	AutoApproveSafe bool
	// Auto-approve read operations
	AutoApproveRead bool
	// Always ask for write operations
	RequireApprovalWrite bool
	// Always ask for execute operations
	RequireApprovalExecute bool
	// Always ask for network operations
	RequireApprovalNetwork bool
	// Blocked commands/patterns for bash
	BlockedCommands []string
}

func DefaultPermissionConfig() *PermissionConfig {
	return &PermissionConfig{
		AutoApproveSafe:        true,
		AutoApproveRead:        true,
		RequireApprovalWrite:   true,
		RequireApprovalExecute: true,
		RequireApprovalNetwork: false,
		BlockedCommands: []string{
			"rm -rf /",
			"dd if=",
			"mkfs",
			":(){ :|:& };:", // Fork bomb
			"> /dev/sda",
		},
	}
}

// ProtectedTool wraps a tool with permission checking
type ProtectedTool struct {
	tool             Tool
	level            PermissionLevel
	checker          PermissionChecker
	permissionConfig *PermissionConfig
}

func NewProtectedTool(tool Tool, level PermissionLevel, checker PermissionChecker, config *PermissionConfig) *ProtectedTool {
	if config == nil {
		config = DefaultPermissionConfig()
	}
	return &ProtectedTool{
		tool:             tool,
		level:            level,
		checker:          checker,
		permissionConfig: config,
	}
}

func (pt *ProtectedTool) Name() string {
	return pt.tool.Name()
}

func (pt *ProtectedTool) Description() string {
	return pt.tool.Description()
}

func (pt *ProtectedTool) Parameters() map[string]interface{} {
	return pt.tool.Parameters()
}

func (pt *ProtectedTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	// Check if approval is needed
	needsApproval := false

	switch pt.level {
	case PermissionSafe:
		needsApproval = !pt.permissionConfig.AutoApproveSafe
	case PermissionRead:
		needsApproval = !pt.permissionConfig.AutoApproveRead
	case PermissionWrite:
		needsApproval = pt.permissionConfig.RequireApprovalWrite
	case PermissionExecute:
		needsApproval = pt.permissionConfig.RequireApprovalExecute
	case PermissionNetwork:
		needsApproval = pt.permissionConfig.RequireApprovalNetwork
	}

	// Special handling for run_command tool
	if pt.tool.Name() == "run_command" {
		if cmd, ok := args["command"].(string); ok {
			// Check blocked commands
			for _, blocked := range pt.permissionConfig.BlockedCommands {
				if contains(cmd, blocked) {
					return "", fmt.Errorf("blocked command pattern detected: %s", blocked)
				}
			}
		}
	}

	if needsApproval && pt.checker != nil {
		details := fmt.Sprintf("Args: %v", args)
		approved, err := pt.checker.RequestPermission(ctx, pt.tool.Name(), pt.level, details)
		if err != nil {
			return "", fmt.Errorf("permission check failed: %w", err)
		}
		if !approved {
			return "", fmt.Errorf("permission denied by user")
		}
	}

	return pt.tool.Execute(ctx, args)
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr || len(s) > len(substr) &&
			(s[:len(substr)] == substr ||
				s[len(s)-len(substr):] == substr ||
				findInString(s, substr)))
}

func findInString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// AutoApproveChecker automatically approves all permission requests (for ACP mode)
type AutoApproveChecker struct{}

func NewAutoApproveChecker() *AutoApproveChecker {
	return &AutoApproveChecker{}
}

func (c *AutoApproveChecker) RequestPermission(ctx context.Context, tool string, level PermissionLevel, details string) (bool, error) {
	// Auto-approve everything in ACP mode - the editor handles permissions
	return true, nil
}
