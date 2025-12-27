package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/LaPingvino/llemecode/internal/tools"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

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
