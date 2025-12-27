package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// CommandWindow is an interactive window for running commands
type CommandWindow struct {
	command   string
	ctx       context.Context
	cancel    context.CancelFunc
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	viewport  viewport.Model
	inputArea textarea.Model
	output    strings.Builder
	mu        sync.Mutex
	running   bool
	exitCode  int
	inputMode bool
	width     int
	height    int
	err       error
}

type oldCommandOutputMsg struct {
	output string
}

type commandExitMsg struct {
	exitCode int
	err      error
}

var (
	commandHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("205")).
				BorderStyle(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("241")).
				Padding(0, 1)

	outputStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252"))

	inputPromptStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("214")).
				Bold(true)
)

// NewCommandWindow creates a new interactive command window
func NewCommandWindow(command string) *CommandWindow {
	ctx, cancel := context.WithCancel(context.Background())

	vp := viewport.New(80, 20)
	vp.SetContent("")

	ta := textarea.New()
	ta.Placeholder = "Type input and press Enter to send to command..."
	ta.SetHeight(3)
	ta.SetWidth(80)
	ta.ShowLineNumbers = false

	return &CommandWindow{
		command:   command,
		ctx:       ctx,
		cancel:    cancel,
		viewport:  vp,
		inputArea: ta,
		running:   false,
		inputMode: false,
	}
}

func (cw *CommandWindow) Init() tea.Cmd {
	return tea.Batch(
		cw.startCommand(),
		cw.waitForOutput(),
	)
}

func (cw *CommandWindow) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			if cw.running {
				cw.cancel() // Kill the command
			}
			return cw, tea.Quit

		case "ctrl+f":
			// Toggle input mode
			if cw.running {
				cw.inputMode = !cw.inputMode
				if cw.inputMode {
					cw.inputArea.Focus()
				} else {
					cw.inputArea.Blur()
				}
			}

		case "enter":
			if cw.inputMode && cw.running {
				// Send input to command
				input := cw.inputArea.Value()
				if cw.stdin != nil {
					cw.stdin.Write([]byte(input + "\n"))
				}
				cw.appendOutput(fmt.Sprintf("\n> %s\n", input))
				cw.inputArea.Reset()
			} else if !cw.running {
				// Command finished, quit on enter
				return cw, tea.Quit
			}

		case "q":
			if !cw.running && !cw.inputMode {
				return cw, tea.Quit
			}
		}

	case tea.WindowSizeMsg:
		cw.width = msg.Width
		cw.height = msg.Height
		cw.viewport.Width = msg.Width - 4
		cw.viewport.Height = msg.Height - 12
		cw.inputArea.SetWidth(msg.Width - 4)

	case oldCommandOutputMsg:
		cw.appendOutput(msg.output)
		if cw.running {
			cmds = append(cmds, cw.waitForOutput())
		}

	case commandExitMsg:
		cw.running = false
		cw.exitCode = msg.exitCode
		cw.err = msg.err
		if msg.err != nil {
			cw.appendOutput(fmt.Sprintf("\n\n❌ Error: %v\n", msg.err))
		}
		cw.appendOutput(fmt.Sprintf("\n\n✓ Command exited with code %d\n", msg.exitCode))
		cw.appendOutput("Press Enter or q to close")
	}

	if cw.inputMode {
		cw.inputArea, cmd = cw.inputArea.Update(msg)
		cmds = append(cmds, cmd)
	} else {
		cw.viewport, cmd = cw.viewport.Update(msg)
		cmds = append(cmds, cmd)
	}

	return cw, tea.Batch(cmds...)
}

func (cw *CommandWindow) View() string {
	var s strings.Builder

	// Header
	status := "Running"
	if !cw.running {
		status = "Finished"
	}
	header := commandHeaderStyle.Render(fmt.Sprintf("⚡ Command: %s [%s]", cw.command, status))
	s.WriteString(header + "\n\n")

	// Output viewport
	s.WriteString(cw.viewport.View() + "\n\n")

	// Input area (if in input mode)
	if cw.inputMode {
		s.WriteString(inputPromptStyle.Render("📝 Input Mode (Ctrl+F to toggle):") + "\n")
		s.WriteString(cw.inputArea.View() + "\n")
	}

	// Help text
	help := ""
	if cw.running {
		if cw.inputMode {
			help = "Enter: send input • Ctrl+F: toggle input • Esc: kill & quit"
		} else {
			help = "Ctrl+F: provide input • ↑/↓: scroll • Esc: kill & quit"
		}
	} else {
		help = "Enter/q: close • ↑/↓: scroll output"
	}

	s.WriteString("\n" + lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Render(help))

	return s.String()
}

func (cw *CommandWindow) startCommand() tea.Cmd {
	return func() tea.Msg {
		cw.mu.Lock()
		cw.running = true
		cw.mu.Unlock()

		cw.cmd = exec.CommandContext(cw.ctx, "bash", "-c", cw.command)

		// Setup stdin for interactive input
		stdin, err := cw.cmd.StdinPipe()
		if err != nil {
			return commandExitMsg{exitCode: -1, err: err}
		}
		cw.stdin = stdin

		// Setup stdout/stderr
		stdout, err := cw.cmd.StdoutPipe()
		if err != nil {
			return commandExitMsg{exitCode: -1, err: err}
		}

		stderr, err := cw.cmd.StderrPipe()
		if err != nil {
			return commandExitMsg{exitCode: -1, err: err}
		}

		// Merge stdout and stderr
		go cw.streamOutput(stdout, "")
		go cw.streamOutput(stderr, "stderr: ")

		if err := cw.cmd.Start(); err != nil {
			return commandExitMsg{exitCode: -1, err: err}
		}

		return nil
	}
}

func (cw *CommandWindow) streamOutput(reader io.Reader, prefix string) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Text()
		if prefix != "" {
			line = prefix + line
		}
		// Send output via channel
		if cw.running {
			cw.appendOutput(line + "\n")
		}
	}
}

func (cw *CommandWindow) waitForOutput() tea.Cmd {
	return func() tea.Msg {
		// Wait for command to finish
		if cw.cmd != nil && cw.cmd.Process != nil {
			err := cw.cmd.Wait()
			exitCode := 0
			if err != nil {
				if exitErr, ok := err.(*exec.ExitError); ok {
					exitCode = exitErr.ExitCode()
				}
			}
			return commandExitMsg{exitCode: exitCode, err: err}
		}
		return nil
	}
}

func (cw *CommandWindow) appendOutput(text string) {
	cw.mu.Lock()
	defer cw.mu.Unlock()

	cw.output.WriteString(text)
	cw.viewport.SetContent(cw.output.String())
	cw.viewport.GotoBottom()
}

// GetOutput returns the full command output
func (cw *CommandWindow) GetOutput() string {
	cw.mu.Lock()
	defer cw.mu.Unlock()
	return cw.output.String()
}

// GetExitCode returns the command exit code
func (cw *CommandWindow) GetExitCode() int {
	return cw.exitCode
}

// RunCommandInteractive runs a command in an interactive window and returns the output
func RunCommandInteractive(command string) (output string, exitCode int, err error) {
	window := NewCommandWindow(command)
	p := tea.NewProgram(window, tea.WithAltScreen())

	finalModel, err := p.Run()
	if err != nil {
		return "", -1, err
	}

	cw := finalModel.(*CommandWindow)
	return cw.GetOutput(), cw.GetExitCode(), cw.err
}

// InteractiveCommandExecutor implements tools.CommandExecutor using interactive windows
type InteractiveCommandExecutor struct{}

func NewInteractiveCommandExecutor() *InteractiveCommandExecutor {
	return &InteractiveCommandExecutor{}
}

func (ice *InteractiveCommandExecutor) Execute(ctx context.Context, command string) (output string, exitCode int, err error) {
	return RunCommandInteractive(command)
}

// SimpleCommandExecutor implements tools.CommandExecutor for non-interactive mode (ACP)
type SimpleCommandExecutor struct{}

func NewSimpleCommandExecutor() *SimpleCommandExecutor {
	return &SimpleCommandExecutor{}
}

func (sce *SimpleCommandExecutor) Execute(ctx context.Context, command string) (output string, exitCode int, err error) {
	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	outputBytes, err := cmd.CombinedOutput()

	exitCode = 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}

	return string(outputBytes), exitCode, err
}

// InlineCommandExecutor executes commands and streams output to the chat UI
type InlineCommandExecutor struct {
	program *tea.Program
}

func NewInlineCommandExecutor(program *tea.Program) *InlineCommandExecutor {
	return &InlineCommandExecutor{
		program: program,
	}
}

func (ice *InlineCommandExecutor) Execute(ctx context.Context, command string) (output string, exitCode int, err error) {
	// Generate unique ID for this command
	id := fmt.Sprintf("cmd_%d", time.Now().UnixNano())

	// Notify UI that command is starting
	ice.program.Send(commandStartMsg{
		id:      id,
		command: command,
	})

	// Execute command
	cmd := exec.CommandContext(ctx, "bash", "-c", command)

	// Capture stdout and stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		ice.program.Send(commandEndMsg{
			id:       id,
			exitCode: -1,
			err:      err,
		})
		return "", -1, err
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		ice.program.Send(commandEndMsg{
			id:       id,
			exitCode: -1,
			err:      err,
		})
		return "", -1, err
	}

	// Start command
	if err := cmd.Start(); err != nil {
		ice.program.Send(commandEndMsg{
			id:       id,
			exitCode: -1,
			err:      err,
		})
		return "", -1, err
	}

	// Stream output
	var outputBuilder strings.Builder
	var wg sync.WaitGroup
	wg.Add(2)

	// Stream stdout
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			outputBuilder.WriteString(line + "\n")
			ice.program.Send(commandOutputMsg{
				id:   id,
				line: line,
			})
		}
	}()

	// Stream stderr
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := "stderr: " + scanner.Text()
			outputBuilder.WriteString(line + "\n")
			ice.program.Send(commandOutputMsg{
				id:   id,
				line: line,
			})
		}
	}()

	// Wait for output streaming to finish
	wg.Wait()

	// Wait for command to finish
	err = cmd.Wait()
	exitCode = 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}

	// Notify UI that command finished
	ice.program.Send(commandEndMsg{
		id:       id,
		exitCode: exitCode,
		err:      err,
	})

	return outputBuilder.String(), exitCode, err
}
