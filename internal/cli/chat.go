package cli

import (
	"context"
	"fmt"
	"runtime"
	"strings"

	"github.com/LaPingvino/llemecode/internal/agent"
	"github.com/LaPingvino/llemecode/internal/config"
	"github.com/LaPingvino/llemecode/internal/logger"
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
	agent                *agent.Agent
	textarea             textarea.Model
	viewport             viewport.Model
	messages             []message
	spinner              spinner.Model
	waiting              bool
	err                  error
	ctx                  context.Context
	width                int
	height               int
	glamour              *glamour.TermRenderer
	bgBenchmark          *BackgroundBenchmark
	benchmarkDone        bool
	commands             *CommandRegistry
	sessionDisabledTools map[string]bool // Session-only disabled tools
	activeBackgroundTask string          // Name of currently running background task
	history              []string        // Command history
	historyIndex         int             // Current position in history (-1 = not browsing)
	searchMode           bool            // Ctrl-R reverse search mode
	searchQuery          string          // Current search query
	searchResults        []int           // Indices in history matching search
	searchIndex          int             // Current position in search results
	statusMessage        string          // Current status message from logger
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

type statusMsg struct {
	message string
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

func RunChat(ctx context.Context, client *ollama.Client, cfg *config.Config, toolRegistry *tools.Registry, bgBenchmark *BackgroundBenchmark) error {
	model := cfg.DefaultModel
	if model == "" {
		return fmt.Errorf("no default model configured. Please run setup first")
	}

	ag := agent.New(client, toolRegistry, cfg, model)

	// Set disabled tools from config
	ag.SetDisabledTools(cfg.DisabledTools)

	// Add system prompt
	if sysPrompt, ok := cfg.SystemPrompts["default"]; ok {
		ag.AddSystemPrompt(sysPrompt)
	} else {
		ag.AddSystemPrompt("")
	}

	// Setup command registry
	cmdRegistry := NewCommandRegistry()
	cmdRegistry.Register(NewHelpCommand(cmdRegistry))
	cmdRegistry.Register(NewListModelsCommand(client, cfg))
	cmdRegistry.Register(NewSwitchModelCommand(client, cfg, toolRegistry))
	cmdRegistry.Register(NewListPromptsCommand(cfg))
	cmdRegistry.Register(NewResetCommand())
	cmdRegistry.Register(NewBenchmarkCommand(client, cfg))
	cmdRegistry.Register(NewConfigCommand())
	cmdRegistry.Register(NewToolsCommand(toolRegistry))
	cmdRegistry.Register(NewAddToolCommand(client, cfg, toolRegistry))
	cmdRegistry.Register(NewAddAllToolsCommand(client, cfg, toolRegistry))
	cmdRegistry.Register(NewRemoveToolCommand(cfg, toolRegistry))
	cmdRegistry.Register(NewEnableToolCommand(cfg, toolRegistry))
	cmdRegistry.Register(NewDisableToolCommand(cfg, toolRegistry))
	cmdRegistry.Register(NewListDisabledToolsCommand(cfg))

	ta := textarea.New()
	ta.Placeholder = "Type your message or /help for commands..."
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
		agent:                ag,
		textarea:             ta,
		viewport:             vp,
		messages:             []message{},
		spinner:              s,
		ctx:                  ctx,
		glamour:              gr,
		bgBenchmark:          bgBenchmark,
		commands:             cmdRegistry,
		sessionDisabledTools: make(map[string]bool),
		history:              []string{},
		historyIndex:         -1,
		searchMode:           false,
	}

	// Add welcome message
	welcomeMsg := fmt.Sprintf("Welcome to Llemecode! You are using **%s**.\n\nAvailable commands:\n- `/help` - Show all commands\n- `/model <name>` - Switch model\n- `/models` - List available models\n- `/reset` - Clear conversation\n\nType your message and press Enter to chat.", model)
	m.messages = append(m.messages, message{
		role:    "system",
		content: welcomeMsg,
	})
	m.updateViewport()

	p := tea.NewProgram(m, tea.WithAltScreen())

	// Set up logger status updater to send status messages to the TUI
	logger.SetStatusUpdater(func(msg string) {
		p.Send(statusMsg{message: msg})
	})

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
		case tea.KeyCtrlC:
			// Exit search mode on Ctrl-C if in search mode
			if m.searchMode {
				m.searchMode = false
				m.searchQuery = ""
				m.searchResults = nil
				m.searchIndex = 0
				return m, nil
			}
			return m, tea.Quit
		case tea.KeyEsc:
			// Exit search mode on Esc if in search mode
			if m.searchMode {
				m.searchMode = false
				m.searchQuery = ""
				m.searchResults = nil
				m.searchIndex = 0
				return m, nil
			}
			return m, tea.Quit
		case tea.KeyCtrlR:
			// Toggle reverse search mode
			if !m.waiting {
				m.searchMode = !m.searchMode
				if m.searchMode {
					m.searchQuery = ""
					m.searchResults = nil
					m.searchIndex = 0
				}
				return m, nil
			}
		case tea.KeyUp:
			// Navigate history backwards
			if !m.waiting && !m.searchMode && len(m.history) > 0 {
				if m.historyIndex == -1 {
					m.historyIndex = len(m.history)
				}
				if m.historyIndex > 0 {
					m.historyIndex--
					m.textarea.SetValue(m.history[m.historyIndex])
				}
				return m, nil
			}
		case tea.KeyDown:
			// Navigate history forwards
			if !m.waiting && !m.searchMode && m.historyIndex != -1 {
				if m.historyIndex < len(m.history)-1 {
					m.historyIndex++
					m.textarea.SetValue(m.history[m.historyIndex])
				} else {
					m.historyIndex = -1
					m.textarea.Reset()
				}
				return m, nil
			}
		case tea.KeyEnter:
			if m.searchMode {
				// Exit search mode and use the current result
				m.searchMode = false
				if len(m.searchResults) > 0 {
					m.textarea.SetValue(m.history[m.searchResults[m.searchIndex]])
					m.historyIndex = m.searchResults[m.searchIndex]
				}
				m.searchQuery = ""
				m.searchResults = nil
				m.searchIndex = 0
				return m, nil
			}
			if !m.waiting && m.textarea.Value() != "" {
				userMsg := m.textarea.Value()

				// Add to history (avoid duplicates of last entry)
				if len(m.history) == 0 || m.history[len(m.history)-1] != userMsg {
					m.history = append(m.history, userMsg)
				}
				m.historyIndex = -1

				m.textarea.Reset()

				// Check if it's a command
				if result, isCmd, err := m.commands.Execute(m.ctx, userMsg, &m); isCmd {
					if err != nil {
						m.messages = append(m.messages, message{
							role:    "error",
							content: fmt.Sprintf("Command error: %v", err),
						})
					} else {
						m.messages = append(m.messages, message{
							role:    "system",
							content: result,
						})
					}
					m.updateViewport()
					return m, nil
				}

				// Regular chat message
				m.messages = append(m.messages, message{role: "user", content: userMsg})
				m.waiting = true
				m.updateViewport()
				return m, tea.Batch(
					m.spinner.Tick,
					m.chat(userMsg),
				)
			}
		default:
			// In search mode, update search query
			if m.searchMode {
				switch msg.String() {
				case "backspace":
					if len(m.searchQuery) > 0 {
						m.searchQuery = m.searchQuery[:len(m.searchQuery)-1]
						m.updateSearchResults()
					}
				case "ctrl+n":
					// Next search result
					if len(m.searchResults) > 0 {
						m.searchIndex = (m.searchIndex + 1) % len(m.searchResults)
					}
				case "ctrl+p":
					// Previous search result
					if len(m.searchResults) > 0 {
						m.searchIndex--
						if m.searchIndex < 0 {
							m.searchIndex = len(m.searchResults) - 1
						}
					}
				default:
					// Regular character input
					if len(msg.String()) == 1 {
						m.searchQuery += msg.String()
						m.updateSearchResults()
					}
				}
				return m, nil
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

	case statusMsg:
		m.statusMessage = msg.message

	case responseMsg:
		logger.Status("Received response: err=%v, tool_calls=%d, content_len=%d", msg.err, len(msg.toolCalls), len(msg.content))
		m.waiting = false
		if msg.err != nil {
			logger.Status("Processing error: %v", msg.err)
			m.err = msg.err
			m.messages = append(m.messages, message{
				role:    "error",
				content: fmt.Sprintf("Error: %v", msg.err),
			})
		} else {
			// Add tool calls if any
			logger.Status("Adding %d tool calls to messages", len(msg.toolCalls))
			for idx, tc := range msg.toolCalls {
				formatted := agent.FormatToolCall(tc)
				logger.Status("Tool call %d formatted, length: %d", idx, len(formatted))
				m.messages = append(m.messages, message{
					role:    "tool",
					content: formatted,
				})
			}

			// Add assistant response
			if msg.content != "" {
				logger.Status("Adding assistant response, length: %d", len(msg.content))
				m.messages = append(m.messages, message{
					role:    "assistant",
					content: msg.content,
				})
			} else {
				logger.Status("No assistant content to add")
			}
		}
		logger.Status("Updating viewport, total messages: %d", len(m.messages))
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

	// Header with memory indicator
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	memMB := float64(memStats.Alloc) / 1024 / 1024

	// Determine memory color based on usage
	memColor := "42" // Green
	if memMB > 500 {
		memColor = "196" // Red
	} else if memMB > 200 {
		memColor = "214" // Orange
	}

	memIndicator := lipgloss.NewStyle().
		Foreground(lipgloss.Color(memColor)).
		Render(fmt.Sprintf(" [%.1f MB]", memMB))

	header := headerStyle.Render(fmt.Sprintf("💬 Llemecode Chat - Model: %s", m.agent.GetMessages()[0].Role)) + memIndicator
	s.WriteString(header + "\n\n")

	// Viewport with messages
	s.WriteString(m.viewport.View() + "\n\n")

	// Status line
	if m.waiting {
		waitMsg := "Thinking..."
		if m.activeBackgroundTask != "" {
			waitMsg = fmt.Sprintf("Running: %s...", m.activeBackgroundTask)
		}
		s.WriteString(m.spinner.View() + " " + waitMsg + "\n")
	} else if m.err != nil {
		s.WriteString(errorStyle.Render(fmt.Sprintf("⚠ %v", m.err)) + "\n")
	} else if m.statusMessage != "" {
		// Show current status message from logger
		s.WriteString(lipgloss.NewStyle().
			Foreground(lipgloss.Color("214")).
			Render(fmt.Sprintf("🔍 %s", m.statusMessage)) + "\n")
	} else if m.activeBackgroundTask != "" {
		// Show background task even when not waiting
		s.WriteString(lipgloss.NewStyle().
			Foreground(lipgloss.Color("214")).
			Render(fmt.Sprintf("⚙️  Background: %s", m.activeBackgroundTask)) + "\n")
	} else if m.bgBenchmark != nil && !m.benchmarkDone {
		// Show background benchmark status
		select {
		case <-m.bgBenchmark.Done():
			m.benchmarkDone = true
			s.WriteString(lipgloss.NewStyle().
				Foreground(lipgloss.Color("42")).
				Render("📊 ✓ Background benchmarking complete!") + "\n")
		default:
			progress := m.bgBenchmark.GetProgress()
			if progress != "" {
				s.WriteString(lipgloss.NewStyle().
					Foreground(lipgloss.Color("241")).
					Render("📊 "+progress) + "\n")
			}
		}
	}

	// Search mode indicator
	if m.searchMode {
		searchStatus := fmt.Sprintf("(reverse-search)`%s': ", m.searchQuery)
		if len(m.searchResults) > 0 {
			preview := m.history[m.searchResults[m.searchIndex]]
			if len(preview) > 50 {
				preview = preview[:47] + "..."
			}
			searchStatus += preview
		} else if m.searchQuery != "" {
			searchStatus += "no matches"
		}
		s.WriteString(lipgloss.NewStyle().
			Foreground(lipgloss.Color("214")).
			Render(searchStatus) + "\n")
	}

	// Textarea
	s.WriteString(m.textarea.View() + "\n")

	// Help
	var help string
	if m.searchMode {
		help = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			Render("Ctrl+N: next • Ctrl+P: prev • Enter: use • Esc: cancel")
	} else {
		help = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			Render("Enter: send • ↑↓: history • Ctrl+R: search • Esc: quit")
	}
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
		case "system":
			rendered := msg.content
			if m.glamour != nil {
				if r, err := m.glamour.Render(msg.content); err == nil {
					rendered = r
				}
			}
			content.WriteString(lipgloss.NewStyle().
				Foreground(lipgloss.Color("111")).
				Render(rendered) + "\n\n")
		}
	}

	m.viewport.SetContent(content.String())
	m.viewport.GotoBottom()
}

func (m chatModel) chat(userMsg string) tea.Cmd {
	return func() tea.Msg {
		logger.Status("Starting agent.Chat call")
		resp, err := m.agent.Chat(m.ctx, userMsg)
		if err != nil {
			logger.Status("agent.Chat returned error: %v", err)
			return responseMsg{err: err}
		}
		logger.Status("agent.Chat successful, content length: %d, tool calls: %d", len(resp.Content), len(resp.ToolCalls))
		return responseMsg{
			content:   resp.Content,
			toolCalls: resp.ToolCalls,
		}
	}
}

// updateSearchResults searches through history for the current query
func (m *chatModel) updateSearchResults() {
	m.searchResults = nil
	if m.searchQuery == "" {
		return
	}

	// Search backwards through history
	for i := len(m.history) - 1; i >= 0; i-- {
		if strings.Contains(strings.ToLower(m.history[i]), strings.ToLower(m.searchQuery)) {
			m.searchResults = append(m.searchResults, i)
		}
	}

	if len(m.searchResults) > 0 {
		m.searchIndex = 0
	}
}

// updateAgentDisabledTools updates the agent with the combined list of disabled tools
func (m *chatModel) updateAgentDisabledTools(cfg *config.Config) {
	// Combine config-level and session-level disabled tools
	disabledMap := make(map[string]bool)

	// Add config-level disabled tools
	for _, toolName := range cfg.DisabledTools {
		disabledMap[toolName] = true
	}

	// Add session-level disabled tools
	for toolName := range m.sessionDisabledTools {
		disabledMap[toolName] = true
	}

	// Convert map back to slice
	disabledList := make([]string, 0, len(disabledMap))
	for toolName := range disabledMap {
		disabledList = append(disabledList, toolName)
	}

	m.agent.SetDisabledTools(disabledList)
}
