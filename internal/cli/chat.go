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
	"golang.org/x/sys/unix"
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

	// Async task management
	currentTask      context.CancelFunc // Cancel function for current task
	messageQueue     []string           // Messages queued while task is running
	processingStatus string             // Current processing status (e.g., "Thinking...", "Running command...")

	// Permission handling
	pendingPermission *permissionRequest // Current permission request awaiting response
	permissionMode    bool               // True when waiting for y/n input

	// Command execution overlay
	activeCommands []*commandExecution // Currently running/recent commands
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

// Command execution tracking
type commandExecution struct {
	id       string   // Unique ID for this command
	command  string   // The command being executed
	output   []string // Lines of output
	running  bool     // Whether still executing
	exitCode int      // Exit code when done
	err      error    // Error if any
}

type commandStartMsg struct {
	id      string
	command string
}

type commandOutputMsg struct {
	id   string
	line string
}

type commandEndMsg struct {
	id       string
	exitCode int
	err      error
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
	cmdRegistry.Register(NewTestToolCommand(toolRegistry))
	cmdRegistry.Register(NewClearQueueCommand())

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

	// Update tool registry to use inline permission checker and command executor
	// This replaces the default ChatPermissionChecker with one integrated into the UI
	toolRegistry.SetPermissionChecker(NewInlineChatPermissionChecker(p))

	// Set inline command executor for run_command tool
	// This streams command output to the UI instead of using a separate window
	inlineExecutor := NewInlineCommandExecutor(p)
	for _, tool := range toolRegistry.All() {
		if tool.Name() == "run_command" {
			if pt, ok := tool.(*tools.ProtectedTool); ok {
				if bashTool, ok := pt.UnwrapTool().(*tools.BashTool); ok {
					bashTool.SetExecutor(inlineExecutor)
				}
			}
			break
		}
	}

	// Set up logger status updater to send status messages to the TUI (non-blocking)
	logger.SetStatusUpdater(func(msg string) {
		// Use goroutine to prevent blocking
		go func() {
			p.Send(statusMsg{message: msg})
		}()
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
		// Handle permission mode first - y/n/c/p/a input
		if m.permissionMode && m.pendingPermission != nil {
			switch msg.String() {
			case "y", "Y":
				m.pendingPermission.response <- permissionResponse{approved: true}
				close(m.pendingPermission.response)
				m.pendingPermission = nil
				m.permissionMode = false
				return m, nil
			case "n", "N":
				m.pendingPermission.response <- permissionResponse{approved: false}
				close(m.pendingPermission.response)
				m.pendingPermission = nil
				m.permissionMode = false
				return m, nil
			case "a", "A":
				// Always allow this tool (no restrictions)
				m.pendingPermission.response <- permissionResponse{approved: true, alwaysTool: true}
				close(m.pendingPermission.response)
				m.pendingPermission = nil
				m.permissionMode = false
				return m, nil
			case "c", "C":
				// For run_command: always allow this specific command
				m.pendingPermission.response <- permissionResponse{approved: true, alwaysCommand: true}
				close(m.pendingPermission.response)
				m.pendingPermission = nil
				m.permissionMode = false
				return m, nil
			case "p", "P":
				// Always allow when using this path/directory
				m.pendingPermission.response <- permissionResponse{approved: true, alwaysPath: true}
				close(m.pendingPermission.response)
				m.pendingPermission = nil
				m.permissionMode = false
				return m, nil
			case "esc":
				// Deny on escape
				m.pendingPermission.response <- permissionResponse{approved: false}
				close(m.pendingPermission.response)
				m.pendingPermission = nil
				m.permissionMode = false
				return m, nil
			}
			// Ignore other keys in permission mode
			return m, nil
		}

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

			// If currently processing and there's a queued message or typed input, interrupt and send
			if m.waiting && m.currentTask != nil {
				// Cancel current task
				m.currentTask()
				m.currentTask = nil

				// Get first queued message or current input
				var interruptMsg string
				if len(m.messageQueue) > 0 {
					interruptMsg = m.messageQueue[0]
					m.messageQueue = m.messageQueue[1:]
				} else {
					interruptMsg = m.textarea.Value()
					m.textarea.Reset()
				}

				if interruptMsg != "" {
					// Add to history
					if len(m.history) == 0 || m.history[len(m.history)-1] != interruptMsg {
						m.history = append(m.history, interruptMsg)
					}
					m.historyIndex = -1

					// Add interrupted notice
					m.messages = append(m.messages, message{
						role:    "system",
						content: "⚠️ Previous task interrupted",
					})

					// Send new message
					m.messages = append(m.messages, message{role: "user", content: interruptMsg})
					m.updateViewport()

					return m, tea.Batch(
						m.spinner.Tick,
						m.chat(interruptMsg),
					)
				}

				// Just cancel without new message
				m.waiting = false
				m.messages = append(m.messages, message{
					role:    "system",
					content: "⚠️ Task cancelled",
				})
				m.updateViewport()
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

			if m.textarea.Value() != "" {
				userMsg := m.textarea.Value()
				m.textarea.Reset()

				// Check if it's a command - execute immediately even if waiting
				if result, isCmd, err := m.commands.Execute(m.ctx, userMsg, &m); isCmd {
					// Add to history
					if len(m.history) == 0 || m.history[len(m.history)-1] != userMsg {
						m.history = append(m.history, userMsg)
					}
					m.historyIndex = -1

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

				// If currently waiting, queue the message instead of sending
				if m.waiting {
					m.messageQueue = append(m.messageQueue, userMsg)
					m.updateViewport() // Update to show queued message indicator
					return m, nil
				}

				// Add to history (avoid duplicates of last entry)
				if len(m.history) == 0 || m.history[len(m.history)-1] != userMsg {
					m.history = append(m.history, userMsg)
				}
				m.historyIndex = -1

				// Regular chat message
				m.messages = append(m.messages, message{role: "user", content: userMsg})
				m.waiting = true
				m.processingStatus = "Thinking..."
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

	case permissionRequestMsg:
		// Store the permission request and enter permission mode
		m.pendingPermission = msg.request
		m.permissionMode = true
		m.processingStatus = "Awaiting permission..."
		return m, nil

	case commandStartMsg:
		// Start tracking a new command
		cmd := &commandExecution{
			id:      msg.id,
			command: msg.command,
			output:  []string{},
			running: true,
		}
		m.activeCommands = append(m.activeCommands, cmd)
		return m, nil

	case commandOutputMsg:
		// Add output line to the appropriate command
		for _, cmd := range m.activeCommands {
			if cmd.id == msg.id {
				cmd.output = append(cmd.output, msg.line)
				break
			}
		}
		return m, nil

	case commandEndMsg:
		// Mark command as finished
		for _, cmd := range m.activeCommands {
			if cmd.id == msg.id {
				cmd.running = false
				cmd.exitCode = msg.exitCode
				cmd.err = msg.err

				// Remove from active list after a short delay (keep for viewing)
				// For now, we'll keep the last 3 commands
				if len(m.activeCommands) > 3 {
					m.activeCommands = m.activeCommands[1:]
				}
				break
			}
		}
		return m, nil

	case responseMsg:
		logger.Status("Received response: err=%v, tool_calls=%d, content_len=%d", msg.err, len(msg.toolCalls), len(msg.content))
		m.waiting = false
		m.processingStatus = ""

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

		// If there are queued messages, send the first one
		if len(m.messageQueue) > 0 {
			queuedMsg := m.messageQueue[0]
			m.messageQueue = m.messageQueue[1:]

			// Add to history
			if len(m.history) == 0 || m.history[len(m.history)-1] != queuedMsg {
				m.history = append(m.history, queuedMsg)
			}

			// Send the queued message
			m.messages = append(m.messages, message{role: "user", content: queuedMsg})
			m.waiting = true
			m.processingStatus = "Thinking..."
			m.updateViewport()

			return m, tea.Batch(
				m.spinner.Tick,
				m.chat(queuedMsg),
			)
		}
	}

	// Always allow textarea updates so user can type anytime
	m.textarea, cmd = m.textarea.Update(msg)
	cmds = append(cmds, cmd)

	m.viewport, cmd = m.viewport.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

func (m chatModel) View() string {
	var s strings.Builder

	// Header without memory indicator (moved to bottom)
	header := headerStyle.Render(fmt.Sprintf("💬 Llemecode Chat - Model: %s", m.agent.GetMessages()[0].Role))
	s.WriteString(header + "\n\n")

	// Viewport with messages
	s.WriteString(m.viewport.View() + "\n\n")

	// Permission prompt (if active)
	if m.permissionMode && m.pendingPermission != nil {
		permBox := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("214")).
			Padding(1, 2).
			Width(m.width - 8)

		levelStr := ""
		levelColor := "214"

		switch m.pendingPermission.level {
		case tools.PermissionExecute:
			levelStr = "⚠️  EXECUTE"
			levelColor = "196"
		case tools.PermissionWrite:
			levelStr = "⚠️  WRITE"
			levelColor = "214"
		case tools.PermissionNetwork:
			levelStr = "🌐 NETWORK"
			levelColor = "214"
		case tools.PermissionRead:
			levelStr = "📖 READ"
			levelColor = "111"
		}

		permContent := lipgloss.NewStyle().
			Foreground(lipgloss.Color(levelColor)).
			Bold(true).
			Render(fmt.Sprintf("%s PERMISSION REQUIRED\n\n", levelStr))

		permContent += fmt.Sprintf("Tool: %s\n", m.pendingPermission.toolName)
		permContent += fmt.Sprintf("Details: %s\n", m.pendingPermission.details)

		// Show target path if available
		if m.pendingPermission.targetPath != "" {
			permContent += fmt.Sprintf("Target: %s\n", m.pendingPermission.targetPath)
		}

		permContent += "\n"
		permContent += lipgloss.NewStyle().
			Foreground(lipgloss.Color("42")).
			Render("Allow this operation?\n")

		// Different options based on tool type
		if m.pendingPermission.toolName == "run_command" {
			if m.pendingPermission.targetPath != "" {
				permContent += lipgloss.NewStyle().
					Foreground(lipgloss.Color("111")).
					Render("  y: yes (once)  n: no  c: always allow command  p: always on this path")
			} else {
				permContent += lipgloss.NewStyle().
					Foreground(lipgloss.Color("111")).
					Render("  y: yes (once)  n: no  a: always allow  c: always allow command")
			}
		} else if m.pendingPermission.targetPath != "" {
			// For file tools with path
			permContent += lipgloss.NewStyle().
				Foreground(lipgloss.Color("111")).
				Render("  y: yes (once)  n: no  a: always allow  p: always on this path")
		} else {
			// Tools without path - only offer "a" for always
			permContent += lipgloss.NewStyle().
				Foreground(lipgloss.Color("111")).
				Render("  y: yes (once)  n: no  a: always allow")
		}

		s.WriteString(permBox.Render(permContent) + "\n\n")
	}

	// Command execution overlay (show running/recent commands)
	if len(m.activeCommands) > 0 {
		for _, cmd := range m.activeCommands {
			cmdBox := lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("205")).
				Padding(0, 1).
				Width(m.width - 8)

			// Command header
			status := "⚡ Running"
			statusColor := "214"
			if !cmd.running {
				if cmd.exitCode == 0 {
					status = "✓ Completed"
					statusColor = "42"
				} else {
					status = "✗ Failed"
					statusColor = "196"
				}
			}

			cmdHeader := lipgloss.NewStyle().
				Foreground(lipgloss.Color(statusColor)).
				Bold(true).
				Render(fmt.Sprintf("%s: %s\n", status, cmd.command))

			// Output (last 10 lines)
			outputLines := cmd.output
			if len(outputLines) > 10 {
				outputLines = outputLines[len(outputLines)-10:]
			}

			cmdOutput := ""
			if len(outputLines) > 0 {
				cmdOutput = "\n" + strings.Join(outputLines, "\n")
			}

			// Exit code if finished
			exitInfo := ""
			if !cmd.running {
				if cmd.err != nil {
					exitInfo = fmt.Sprintf("\nExit code: %d (error: %v)", cmd.exitCode, cmd.err)
				} else {
					exitInfo = fmt.Sprintf("\nExit code: %d", cmd.exitCode)
				}
			}

			s.WriteString(cmdBox.Render(cmdHeader+cmdOutput+exitInfo) + "\n\n")
		}
	}

	// Status line
	if m.waiting {
		waitMsg := m.processingStatus
		if waitMsg == "" {
			waitMsg = "Thinking..."
		}
		if m.activeBackgroundTask != "" {
			waitMsg = fmt.Sprintf("Running: %s...", m.activeBackgroundTask)
		}
		statusLine := m.spinner.View() + " " + waitMsg

		// Show queued messages indicator
		if len(m.messageQueue) > 0 {
			queueInfo := fmt.Sprintf(" | ⏸ Queue: %d msg", len(m.messageQueue))
			if len(m.messageQueue) > 1 {
				queueInfo += "s"
			}
			// Show preview of first queued message
			queuePreview := m.messageQueue[0]
			if len(queuePreview) > 30 {
				queuePreview = queuePreview[:27] + "..."
			}
			queueInfo += fmt.Sprintf(" (%s) | Esc: interrupt", queuePreview)

			statusLine += lipgloss.NewStyle().
				Foreground(lipgloss.Color("214")).
				Render(queueInfo)
		}
		s.WriteString(statusLine + "\n")
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

	// Help line with RAM indicator
	var help string
	memIndicator := m.getMemoryIndicator()

	if m.searchMode {
		help = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			Render("Ctrl+N: next • Ctrl+P: prev • Enter: use • Esc: cancel")
	} else if m.permissionMode {
		// Context-aware help based on tool and available options
		if m.pendingPermission != nil {
			if m.pendingPermission.toolName == "run_command" {
				if m.pendingPermission.targetPath != "" {
					help = lipgloss.NewStyle().
						Foreground(lipgloss.Color("241")).
						Render("y: once • n: deny • c: always this cmd • p: always this path • Esc: deny")
				} else {
					help = lipgloss.NewStyle().
						Foreground(lipgloss.Color("241")).
						Render("y: once • n: deny • a: always allow tool • c: always this cmd • Esc: deny")
				}
			} else if m.pendingPermission.targetPath != "" {
				help = lipgloss.NewStyle().
					Foreground(lipgloss.Color("241")).
					Render("y: once • n: deny • a: always allow tool • p: always this path • Esc: deny")
			} else {
				help = lipgloss.NewStyle().
					Foreground(lipgloss.Color("241")).
					Render("y: once • n: deny • a: always allow tool • Esc: deny")
			}
		} else {
			help = lipgloss.NewStyle().
				Foreground(lipgloss.Color("241")).
				Render("y: approve • n: deny • Esc: deny")
		}
	} else if m.waiting && len(m.messageQueue) > 0 {
		help = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			Render("Esc: interrupt and send next queued • /clear-queue to clear")
	} else if m.waiting {
		help = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			Render("Enter: queue message • /cmd: runs immediately • Esc: interrupt")
	} else {
		help = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			Render("Enter: send • ↑↓: history • Ctrl+R: search • Esc: quit")
	}

	s.WriteString("\n" + help + " " + memIndicator)

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

func (m *chatModel) chat(userMsg string) tea.Cmd {
	return func() tea.Msg {
		// Create cancellable context for this chat
		taskCtx, cancel := context.WithCancel(m.ctx)
		m.currentTask = cancel

		logger.Status("Starting agent.Chat call")
		resp, err := m.agent.Chat(taskCtx, userMsg)

		// Clear the task when done
		m.currentTask = nil

		if err != nil {
			// Check if it was cancelled
			if taskCtx.Err() == context.Canceled {
				logger.Status("agent.Chat was cancelled")
				return responseMsg{err: fmt.Errorf("task cancelled")}
			}
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

// getMemoryIndicator returns a formatted memory usage indicator showing system memory
func (m *chatModel) getMemoryIndicator() string {
	// Get system memory info
	var sysinfo unix.Sysinfo_t
	if err := unix.Sysinfo(&sysinfo); err != nil {
		// Fallback to process memory if system info unavailable
		var memStats runtime.MemStats
		runtime.ReadMemStats(&memStats)
		memMB := float64(memStats.Alloc) / 1024 / 1024
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			Render(fmt.Sprintf("[%.1f MB]", memMB))
	}

	// Calculate total and used memory in GB
	totalGB := float64(sysinfo.Totalram*uint64(sysinfo.Unit)) / 1024 / 1024 / 1024
	freeGB := float64(sysinfo.Freeram*uint64(sysinfo.Unit)) / 1024 / 1024 / 1024
	usedGB := totalGB - freeGB
	usagePercent := (usedGB / totalGB) * 100

	// Determine color based on usage percentage
	memColor := "42" // Green
	if usagePercent > 90 {
		memColor = "196" // Red
	} else if usagePercent > 75 {
		memColor = "214" // Orange
	}

	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(memColor)).
		Render(fmt.Sprintf("[%.1f/%.1f GB %.0f%%]", usedGB, totalGB, usagePercent))
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
