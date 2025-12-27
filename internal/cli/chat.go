package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/LaPingvino/llemecode/internal/agent"
	"github.com/LaPingvino/llemecode/internal/config"
	"github.com/LaPingvino/llemecode/internal/ollama"
	"github.com/LaPingvino/llemecode/internal/tools"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

type chatModel struct {
	agent    *agent.Agent
	textarea textarea.Model
	viewport viewport.Model
	messages []message
	spinner  spinner.Model
	waiting  bool
	err      error
	ctx      context.Context
	width    int
	height   int
	glamour  *glamour.TermRenderer
}

type message struct {
	role    string
	content string
}

type responseMsg struct {
	content   string
	toolCalls []agent.ToolExecution
	err       error
}

var (
	userStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("86")).
			Bold(true)

	assistantStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("213"))

	toolStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("229")).
			Italic(true)

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			Bold(true)

	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("205")).
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("241")).
			Padding(0, 1)
)

func RunChat(ctx context.Context, client *ollama.Client, cfg *config.Config, toolRegistry *tools.Registry) error {
	model := cfg.DefaultModel
	if model == "" {
		return fmt.Errorf("no default model configured. Please run setup first")
	}

	ag := agent.New(client, toolRegistry, cfg, model)

	// Add system prompt
	if sysPrompt, ok := cfg.SystemPrompts["default"]; ok {
		ag.AddSystemPrompt(sysPrompt)
	} else {
		ag.AddSystemPrompt("")
	}

	ta := textarea.New()
	ta.Placeholder = "Type your message..."
	ta.Focus()
	ta.CharLimit = 4000
	ta.SetWidth(80)
	ta.SetHeight(3)

	vp := viewport.New(80, 20)
	vp.SetContent("")

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	gr, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(78),
	)
	if err != nil {
		gr = nil
	}

	m := chatModel{
		agent:    ag,
		textarea: ta,
		viewport: vp,
		messages: []message{},
		spinner:  s,
		ctx:      ctx,
		glamour:  gr,
	}

	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err = p.Run()
	return err
}

func (m chatModel) Init() tea.Cmd {
	return tea.Batch(
		textarea.Blink,
		m.spinner.Tick,
	)
}

func (m chatModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit
		case tea.KeyEnter:
			if !m.waiting && m.textarea.Value() != "" {
				userMsg := m.textarea.Value()
				m.messages = append(m.messages, message{role: "user", content: userMsg})
				m.textarea.Reset()
				m.waiting = true
				m.updateViewport()
				return m, tea.Batch(
					m.spinner.Tick,
					m.chat(userMsg),
				)
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.viewport.Width = msg.Width - 4
		m.viewport.Height = msg.Height - 8
		m.textarea.SetWidth(msg.Width - 4)
		m.updateViewport()

	case spinner.TickMsg:
		if m.waiting {
			m.spinner, cmd = m.spinner.Update(msg)
			cmds = append(cmds, cmd)
		}

	case responseMsg:
		m.waiting = false
		if msg.err != nil {
			m.err = msg.err
			m.messages = append(m.messages, message{
				role:    "error",
				content: fmt.Sprintf("Error: %v", msg.err),
			})
		} else {
			// Add tool calls if any
			for _, tc := range msg.toolCalls {
				m.messages = append(m.messages, message{
					role:    "tool",
					content: agent.FormatToolCall(tc),
				})
			}

			// Add assistant response
			if msg.content != "" {
				m.messages = append(m.messages, message{
					role:    "assistant",
					content: msg.content,
				})
			}
		}
		m.updateViewport()
	}

	if !m.waiting {
		m.textarea, cmd = m.textarea.Update(msg)
		cmds = append(cmds, cmd)
	}

	m.viewport, cmd = m.viewport.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

func (m chatModel) View() string {
	var s strings.Builder

	// Header
	header := headerStyle.Render(fmt.Sprintf("💬 Llemecode Chat - Model: %s", m.agent.GetMessages()[0].Role))
	s.WriteString(header + "\n\n")

	// Viewport with messages
	s.WriteString(m.viewport.View() + "\n\n")

	// Status line
	if m.waiting {
		s.WriteString(m.spinner.View() + " Thinking...\n")
	} else if m.err != nil {
		s.WriteString(errorStyle.Render(fmt.Sprintf("⚠ %v", m.err)) + "\n")
	}

	// Textarea
	s.WriteString(m.textarea.View() + "\n")

	// Help
	help := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Render("Enter: send • Esc: quit")
	s.WriteString("\n" + help)

	return s.String()
}

func (m *chatModel) updateViewport() {
	var content strings.Builder

	for _, msg := range m.messages {
		switch msg.role {
		case "user":
			content.WriteString(userStyle.Render("You: ") + msg.content + "\n\n")
		case "assistant":
			rendered := msg.content
			if m.glamour != nil {
				if r, err := m.glamour.Render(msg.content); err == nil {
					rendered = r
				}
			}
			content.WriteString(assistantStyle.Render("Assistant: ") + "\n" + rendered + "\n")
		case "tool":
			content.WriteString(toolStyle.Render(msg.content) + "\n")
		case "error":
			content.WriteString(errorStyle.Render(msg.content) + "\n\n")
		}
	}

	m.viewport.SetContent(content.String())
	m.viewport.GotoBottom()
}

func (m chatModel) chat(userMsg string) tea.Cmd {
	return func() tea.Msg {
		resp, err := m.agent.Chat(m.ctx, userMsg)
		if err != nil {
			return responseMsg{err: err}
		}
		return responseMsg{
			content:   resp.Content,
			toolCalls: resp.ToolCalls,
		}
	}
}
