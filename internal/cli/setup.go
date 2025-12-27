package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/LaPingvino/llemecode/internal/benchmark"
	"github.com/LaPingvino/llemecode/internal/config"
	"github.com/LaPingvino/llemecode/internal/ollama"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type setupModel struct {
	client      *ollama.Client
	cfg         *config.Config
	spinner     spinner.Model
	status      string
	logs        []string
	done        bool
	err         error
	ctx         context.Context
	progressCh  chan string
	benchmarker *benchmark.Benchmarker
}

type progressMsg string
type doneMsg struct {
	err error
}

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("205")).
			MarginLeft(2)

	statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			MarginLeft(2)

	logStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252")).
			MarginLeft(4)

	successStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("42")).
			Bold(true).
			MarginLeft(2)
)

func RunSetup(ctx context.Context, client *ollama.Client, cfg *config.Config) error {
	progressCh := make(chan string, 100)

	benchmarker := benchmark.New(client, cfg.BenchmarkTasks)

	// If a default model is set, use it as the evaluator
	if cfg.DefaultModel != "" {
		progressCh <- fmt.Sprintf("Using %s to evaluate other models", cfg.DefaultModel)
		benchmarker.SetEvaluator(cfg.DefaultModel)
	}

	m := setupModel{
		client:      client,
		cfg:         cfg,
		spinner:     spinner.New(),
		ctx:         ctx,
		progressCh:  progressCh,
		status:      "Initializing...",
		logs:        []string{},
		benchmarker: benchmarker,
	}

	m.spinner.Spinner = spinner.Dot
	m.spinner.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	p := tea.NewProgram(m)

	// Start benchmarking in background
	go func() {
		scores, err := m.benchmarker.BenchmarkAll(ctx, progressCh)
		if err != nil {
			p.Send(doneMsg{err: err})
			return
		}

		// Update config with benchmark results
		m.benchmarker.UpdateConfig(cfg, scores)

		// Save config
		configDir, _ := config.GetConfigDir()
		progressCh <- fmt.Sprintf("\nSaving configuration to %s", configDir)

		if err := cfg.Save(); err != nil {
			p.Send(doneMsg{err: err})
			return
		}

		// Save benchmark results
		resultsPath := configDir + "/benchmark_results.json"
		if err := m.benchmarker.SaveResults(scores, resultsPath); err != nil {
			progressCh <- fmt.Sprintf("Warning: Could not save benchmark results: %v", err)
		}

		progressCh <- fmt.Sprintf("\n✓ Setup complete! Default model: %s", cfg.DefaultModel)
		p.Send(doneMsg{err: nil})
	}()

	_, err := p.Run()
	close(progressCh)
	return err
}

func (m setupModel) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		waitForProgress(m.progressCh),
	)
}

func (m setupModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" || msg.String() == "q" {
			return m, tea.Quit
		}

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case progressMsg:
		m.status = string(msg)
		m.logs = append(m.logs, string(msg))
		// Keep only last 15 lines
		if len(m.logs) > 15 {
			m.logs = m.logs[len(m.logs)-15:]
		}
		return m, waitForProgress(m.progressCh)

	case doneMsg:
		m.done = true
		m.err = msg.err
		return m, tea.Quit
	}

	return m, nil
}

func (m setupModel) View() string {
	var s strings.Builder

	s.WriteString(titleStyle.Render("🚀 Llemecode First-Run Setup"))
	s.WriteString("\n\n")

	if m.done {
		if m.err != nil {
			s.WriteString(lipgloss.NewStyle().
				Foreground(lipgloss.Color("196")).
				Render(fmt.Sprintf("❌ Setup failed: %v\n", m.err)))
		} else {
			s.WriteString(successStyle.Render("✓ Setup completed successfully!\n"))
			s.WriteString(statusStyle.Render("You can now start using llemecode.\n"))
		}
		return s.String()
	}

	s.WriteString(fmt.Sprintf("%s %s\n\n", m.spinner.View(), statusStyle.Render(m.status)))

	// Show recent logs
	if len(m.logs) > 0 {
		s.WriteString(statusStyle.Render("Progress:") + "\n")
		for _, log := range m.logs {
			s.WriteString(logStyle.Render(log) + "\n")
		}
	}

	s.WriteString("\n" + statusStyle.Render("Press Ctrl+C to cancel"))

	return s.String()
}

func waitForProgress(progressCh chan string) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-progressCh
		if !ok {
			return nil
		}
		return progressMsg(msg)
	}
}
