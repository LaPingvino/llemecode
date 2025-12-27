package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/LaPingvino/llemecode/internal/config"
	"github.com/LaPingvino/llemecode/internal/tools"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Permission request types (shared with chat.go)
type permissionRequest struct {
	toolName   string
	level      tools.PermissionLevel
	details    string
	targetPath string // Path being accessed (for "always allow")
	response   chan permissionResponse
}

type permissionResponse struct {
	approved      bool
	alwaysTool    bool // Always allow this tool (no restrictions)
	alwaysCommand bool // For run_command: always allow this specific command
	alwaysPath    bool // Always allow when using this path/directory
}

type permissionRequestMsg struct {
	request *permissionRequest
}

type PermissionPrompt struct {
	toolName string
	level    tools.PermissionLevel
	details  string
	approved bool
	answered bool
}

type permissionResponseMsg struct {
	approved bool
}

var (
	warningStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("208")).
			Bold(true)

	dangerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			Bold(true)
)

func (pp PermissionPrompt) Init() tea.Cmd {
	return nil
}

func (pp PermissionPrompt) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "y", "Y":
			pp.approved = true
			pp.answered = true
			return pp, tea.Quit
		case "n", "N", "esc":
			pp.approved = false
			pp.answered = true
			return pp, tea.Quit
		}
	}
	return pp, nil
}

func (pp PermissionPrompt) View() string {
	var s strings.Builder

	levelStr := ""
	style := warningStyle

	switch pp.level {
	case tools.PermissionExecute:
		levelStr = "⚠️  EXECUTE"
		style = dangerStyle
	case tools.PermissionWrite:
		levelStr = "⚠️  WRITE"
		style = warningStyle
	case tools.PermissionNetwork:
		levelStr = "🌐 NETWORK"
		style = warningStyle
	case tools.PermissionRead:
		levelStr = "📖 READ"
	}

	s.WriteString(style.Render(fmt.Sprintf("\n%s PERMISSION REQUIRED\n", levelStr)))
	s.WriteString("\n")
	s.WriteString(fmt.Sprintf("Tool: %s\n", pp.toolName))
	s.WriteString(fmt.Sprintf("Details: %s\n\n", pp.details))
	s.WriteString("Allow this operation? (y/n): ")

	return s.String()
}

// ChatPermissionChecker implements PermissionChecker for the chat interface
type ChatPermissionChecker struct {
	program *tea.Program
}

func NewChatPermissionChecker() *ChatPermissionChecker {
	return &ChatPermissionChecker{}
}

func (cpc *ChatPermissionChecker) RequestPermission(ctx context.Context, tool string, level tools.PermissionLevel, details string) (bool, error) {
	prompt := PermissionPrompt{
		toolName: tool,
		level:    level,
		details:  details,
	}

	p := tea.NewProgram(prompt)
	finalModel, err := p.Run()
	if err != nil {
		return false, err
	}

	result := finalModel.(PermissionPrompt)
	return result.approved, nil
}

// InlineChatPermissionChecker sends permission requests to the main chat UI
type InlineChatPermissionChecker struct {
	program *tea.Program
}

func NewInlineChatPermissionChecker(program *tea.Program) *InlineChatPermissionChecker {
	return &InlineChatPermissionChecker{
		program: program,
	}
}

func (icpc *InlineChatPermissionChecker) RequestPermission(ctx context.Context, tool string, level tools.PermissionLevel, details string) (bool, error) {
	// Extract target path from details if present
	targetPath := extractPathFromDetails(tool, details)

	// Create permission request with response channel
	request := &permissionRequest{
		toolName:   tool,
		level:      level,
		details:    details,
		targetPath: targetPath,
		response:   make(chan permissionResponse, 1),
	}

	// Send permission request to chat UI
	icpc.program.Send(permissionRequestMsg{request: request})

	// Wait for response or context cancellation
	select {
	case resp := <-request.response:
		// TODO: Save permission patterns if needed
		if resp.alwaysTool || resp.alwaysCommand || resp.alwaysPath {
			// Save to config
			savePermissionPattern(tool, details, targetPath, resp)
		}
		return resp.approved, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

// extractPathFromDetails attempts to extract a file path or directory from the tool details
func extractPathFromDetails(tool, details string) string {
	switch tool {
	case "read_file", "write_file", "list_directory":
		// These tools typically have the path in the details string
		// Look for common patterns like "File: /path/to/file" or "Directory: /path/to/dir"
		if strings.Contains(details, "File: ") {
			parts := strings.SplitN(details, "File: ", 2)
			if len(parts) == 2 {
				// Extract until newline or end
				path := strings.Split(parts[1], "\n")[0]
				return strings.TrimSpace(path)
			}
		}
		if strings.Contains(details, "Directory: ") {
			parts := strings.SplitN(details, "Directory: ", 2)
			if len(parts) == 2 {
				path := strings.Split(parts[1], "\n")[0]
				return strings.TrimSpace(path)
			}
		}
		if strings.Contains(details, "Path: ") {
			parts := strings.SplitN(details, "Path: ", 2)
			if len(parts) == 2 {
				path := strings.Split(parts[1], "\n")[0]
				return strings.TrimSpace(path)
			}
		}
	case "run_command":
		// For commands, extract the first path-like argument
		// Look for patterns like "Command: ls /path/to/dir"
		if strings.Contains(details, "Command: ") {
			parts := strings.SplitN(details, "Command: ", 2)
			if len(parts) == 2 {
				cmdLine := strings.Split(parts[1], "\n")[0]
				// Split by spaces and look for path-like arguments
				args := strings.Fields(cmdLine)
				for _, arg := range args {
					// Check if it looks like a path (starts with /, ./, ../, or ~)
					if strings.HasPrefix(arg, "/") || strings.HasPrefix(arg, "./") ||
						strings.HasPrefix(arg, "../") || strings.HasPrefix(arg, "~") {
						return arg
					}
				}
			}
		}
	}
	return ""
}

// savePermissionPattern saves a permission pattern to the config
func savePermissionPattern(tool, details, targetPath string, resp permissionResponse) {
	// Load current config
	cfg, err := config.Load()
	if err != nil {
		// Log error but don't fail - permission was already granted for this operation
		fmt.Printf("Warning: Failed to save permission pattern: %v\n", err)
		return
	}

	// Create new pattern based on response type
	var pattern config.PermissionPattern
	pattern.Tool = tool
	pattern.Enabled = true

	if resp.alwaysTool {
		// Always allow this tool, no restrictions
		pattern.AlwaysAllow = true
	} else if resp.alwaysCommand && tool == "run_command" {
		// Extract command prefix (first word) from details
		command := extractCommandFromDetails(details)
		if command != "" {
			pattern.CommandPattern = command
		} else {
			// Fallback to always allow if we can't extract command
			pattern.AlwaysAllow = true
		}
	} else if resp.alwaysPath && targetPath != "" {
		// Use the target path as a pattern
		pattern.PathPattern = targetPath
	} else {
		// Invalid combination, don't save
		return
	}

	// Check if this pattern already exists
	for _, existing := range cfg.Permissions.AlwaysAllowPatterns {
		if existing.Tool == pattern.Tool &&
			existing.PathPattern == pattern.PathPattern &&
			existing.CommandPattern == pattern.CommandPattern &&
			existing.AlwaysAllow == pattern.AlwaysAllow {
			// Pattern already exists, no need to save again
			return
		}
	}

	// Add pattern to config
	cfg.Permissions.AlwaysAllowPatterns = append(cfg.Permissions.AlwaysAllowPatterns, pattern)

	// Save config
	if err := cfg.Save(); err != nil {
		fmt.Printf("Warning: Failed to save config: %v\n", err)
	}
}

// extractCommandFromDetails extracts the command name from the details string
func extractCommandFromDetails(details string) string {
	// Look for "Command: <cmd>" pattern
	if strings.Contains(details, "Command: ") {
		parts := strings.SplitN(details, "Command: ", 2)
		if len(parts) == 2 {
			cmdLine := strings.Split(parts[1], "\n")[0]
			// Get first word (the actual command)
			fields := strings.Fields(cmdLine)
			if len(fields) > 0 {
				return fields[0]
			}
		}
	}
	return ""
}
