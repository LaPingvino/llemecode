package cli

import (
	"context"
	"fmt"

	"github.com/LaPingvino/llemecode/internal/ollama"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type modelPickerModel struct {
	models   []ollama.ModelInfo
	cursor   int
	selected int
	done     bool
	err      error
}

type modelSelectedMsg struct {
	model string
}

var (
	selectedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("205")).
			Bold(true)

	cursorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("86"))
)

func RunModelPicker(ctx context.Context, client *ollama.Client) (string, error) {
	models, err := client.ListModels(ctx)
	if err != nil {
		return "", fmt.Errorf("list models: %w", err)
	}

	if len(models) == 0 {
		return "", fmt.Errorf("no models found. Please pull at least one model with 'ollama pull <model>'")
	}

	m := modelPickerModel{
		models:   models,
		selected: -1,
	}

	p := tea.NewProgram(m)
	finalModel, err := p.Run()
	if err != nil {
		return "", err
	}

	result := finalModel.(modelPickerModel)
	if result.err != nil {
		return "", result.err
	}

	if result.selected >= 0 && result.selected < len(result.models) {
		return result.models[result.selected].Name, nil
	}

	return "", fmt.Errorf("no model selected")
}

func (m modelPickerModel) Init() tea.Cmd {
	return nil
}

func (m modelPickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			m.err = fmt.Errorf("cancelled")
			m.done = true
			return m, tea.Quit

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}

		case "down", "j":
			if m.cursor < len(m.models)-1 {
				m.cursor++
			}

		case "enter", " ":
			m.selected = m.cursor
			m.done = true
			return m, tea.Quit
		}
	}

	return m, nil
}

func (m modelPickerModel) View() string {
	if m.done {
		return ""
	}

	s := titleStyle.Render("🚀 Welcome to Llemecode!") + "\n\n"
	s += statusStyle.Render("Select a model to start with:") + "\n\n"

	for i, model := range m.models {
		cursor := " "
		if m.cursor == i {
			cursor = cursorStyle.Render(">")
		}

		modelName := model.Name
		if m.cursor == i {
			modelName = selectedStyle.Render(modelName)
		}

		// Format size
		sizeMB := float64(model.Size) / 1024 / 1024
		sizeStr := fmt.Sprintf("%.1f MB", sizeMB)
		if sizeMB > 1024 {
			sizeStr = fmt.Sprintf("%.1f GB", sizeMB/1024)
		}

		s += fmt.Sprintf("%s %s %s\n", cursor, modelName,
			lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render(fmt.Sprintf("(%s)", sizeStr)))
	}

	s += "\n" + statusStyle.Render("↑/↓: navigate • Enter: select • q: quit")
	s += "\n" + lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Render("Benchmarking will run in the background while you chat.")

	return s
}
