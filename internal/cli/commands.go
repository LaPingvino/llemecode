package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/LaPingvino/llemecode/internal/agent"
	"github.com/LaPingvino/llemecode/internal/benchmark"
	"github.com/LaPingvino/llemecode/internal/config"
	"github.com/LaPingvino/llemecode/internal/ollama"
	"github.com/LaPingvino/llemecode/internal/tools"
)

type Command interface {
	Name() string
	Description() string
	Execute(ctx context.Context, args []string, m *chatModel) (string, error)
}

type CommandRegistry struct {
	commands map[string]Command
}

func NewCommandRegistry() *CommandRegistry {
	return &CommandRegistry{
		commands: make(map[string]Command),
	}
}

func (cr *CommandRegistry) Register(cmd Command) {
	cr.commands[cmd.Name()] = cmd
}

func (cr *CommandRegistry) Get(name string) (Command, bool) {
	cmd, ok := cr.commands[name]
	return cmd, ok
}

func (cr *CommandRegistry) List() []Command {
	cmds := make([]Command, 0, len(cr.commands))
	for _, cmd := range cr.commands {
		cmds = append(cmds, cmd)
	}
	return cmds
}

func (cr *CommandRegistry) Execute(ctx context.Context, input string, m *chatModel) (string, bool, error) {
	if !strings.HasPrefix(input, "/") {
		return "", false, nil
	}

	parts := strings.Fields(input)
	if len(parts) == 0 {
		return "", false, nil
	}

	cmdName := strings.TrimPrefix(parts[0], "/")
	args := parts[1:]

	cmd, ok := cr.Get(cmdName)
	if !ok {
		return "", true, fmt.Errorf("unknown command: /%s (type /help for available commands)", cmdName)
	}

	result, err := cmd.Execute(ctx, args, m)
	return result, true, err
}

// HelpCommand
type HelpCommand struct {
	registry *CommandRegistry
}

func NewHelpCommand(registry *CommandRegistry) *HelpCommand {
	return &HelpCommand{registry: registry}
}

func (c *HelpCommand) Name() string {
	return "help"
}

func (c *HelpCommand) Description() string {
	return "Show available commands"
}

func (c *HelpCommand) Execute(ctx context.Context, args []string, m *chatModel) (string, error) {
	var sb strings.Builder
	sb.WriteString("## Available Commands\n\n")

	for _, cmd := range c.registry.List() {
		sb.WriteString(fmt.Sprintf("- **/%s** - %s\n", cmd.Name(), cmd.Description()))
	}

	return sb.String(), nil
}

// ListModelsCommand
type ListModelsCommand struct {
	client *ollama.Client
	cfg    *config.Config
}

func NewListModelsCommand(client *ollama.Client, cfg *config.Config) *ListModelsCommand {
	return &ListModelsCommand{client: client, cfg: cfg}
}

func (c *ListModelsCommand) Name() string {
	return "models"
}

func (c *ListModelsCommand) Description() string {
	return "List available models"
}

func (c *ListModelsCommand) Execute(ctx context.Context, args []string, m *chatModel) (string, error) {
	models, err := c.client.ListModels(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to list models: %w", err)
	}

	var sb strings.Builder
	sb.WriteString("## Available Models\n\n")

	for _, model := range models {
		marker := " "
		if model.Name == c.cfg.DefaultModel {
			marker = "★"
		}

		sb.WriteString(fmt.Sprintf("- %s **%s**", marker, model.Name))

		if cap, ok := c.cfg.ModelCapabilities[model.Name]; ok {
			sb.WriteString(fmt.Sprintf(" _(format: %s", cap.ToolCallFormat))
			if len(cap.RecommendedFor) > 0 {
				sb.WriteString(fmt.Sprintf(", good for: %s", strings.Join(cap.RecommendedFor, ", ")))
			}
			sb.WriteString(")_")
		}

		sb.WriteString("\n")
	}

	return sb.String(), nil
}

// SwitchModelCommand
type SwitchModelCommand struct {
	client       *ollama.Client
	cfg          *config.Config
	toolRegistry *tools.Registry
}

func NewSwitchModelCommand(client *ollama.Client, cfg *config.Config, toolRegistry *tools.Registry) *SwitchModelCommand {
	return &SwitchModelCommand{client: client, cfg: cfg, toolRegistry: toolRegistry}
}

func (c *SwitchModelCommand) Name() string {
	return "model"
}

func (c *SwitchModelCommand) Description() string {
	return "Switch to a different model (usage: /model <model-name>)"
}

func (c *SwitchModelCommand) Execute(ctx context.Context, args []string, m *chatModel) (string, error) {
	if len(args) == 0 {
		return fmt.Sprintf("Current model: %s\nUsage: /model <model-name>", c.cfg.DefaultModel), nil
	}

	newModel := args[0]

	// Verify model exists
	models, err := c.client.ListModels(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to verify model: %w", err)
	}

	found := false
	for _, model := range models {
		if model.Name == newModel {
			found = true
			break
		}
	}

	if !found {
		return "", fmt.Errorf("model '%s' not found. Use /models to see available models", newModel)
	}

	// Update config
	c.cfg.DefaultModel = newModel
	if err := c.cfg.Save(); err != nil {
		return "", fmt.Errorf("failed to save config: %w", err)
	}

	// Create new agent with the new model
	m.agent = agent.New(c.client, c.toolRegistry, c.cfg, newModel)
	if sysPrompt, ok := c.cfg.SystemPrompts["default"]; ok {
		m.agent.AddSystemPrompt(sysPrompt)
	} else {
		m.agent.AddSystemPrompt("")
	}

	return fmt.Sprintf("✓ Switched to model: %s", newModel), nil
}

// ListPromptsCommand
type ListPromptsCommand struct {
	cfg *config.Config
}

func NewListPromptsCommand(cfg *config.Config) *ListPromptsCommand {
	return &ListPromptsCommand{cfg: cfg}
}

func (c *ListPromptsCommand) Name() string {
	return "prompts"
}

func (c *ListPromptsCommand) Description() string {
	return "List available system prompts"
}

func (c *ListPromptsCommand) Execute(ctx context.Context, args []string, m *chatModel) (string, error) {
	var sb strings.Builder
	sb.WriteString("Available system prompts:\n\n")

	for name, prompt := range c.cfg.SystemPrompts {
		preview := prompt
		if len(preview) > 100 {
			preview = preview[:100] + "..."
		}
		sb.WriteString(fmt.Sprintf("• %s:\n  %s\n\n", name, preview))
	}

	sb.WriteString("Edit prompts in: ~/.config/llemecode/config.json")

	return sb.String(), nil
}

// ResetCommand
type ResetCommand struct{}

func NewResetCommand() *ResetCommand {
	return &ResetCommand{}
}

func (c *ResetCommand) Name() string {
	return "reset"
}

func (c *ResetCommand) Description() string {
	return "Clear conversation history"
}

func (c *ResetCommand) Execute(ctx context.Context, args []string, m *chatModel) (string, error) {
	m.agent.ClearHistory()
	m.messages = []message{}
	m.updateViewport()
	return "✓ Conversation cleared", nil
}

// BenchmarkCommand
type BenchmarkCommand struct {
	client *ollama.Client
	cfg    *config.Config
}

func NewBenchmarkCommand(client *ollama.Client, cfg *config.Config) *BenchmarkCommand {
	return &BenchmarkCommand{client: client, cfg: cfg}
}

func (c *BenchmarkCommand) Name() string {
	return "benchmark"
}

func (c *BenchmarkCommand) Description() string {
	return "Run benchmarks in background"
}

func (c *BenchmarkCommand) Execute(ctx context.Context, args []string, m *chatModel) (string, error) {
	if m.bgBenchmark != nil && m.bgBenchmark.IsRunning() {
		return "Benchmarking is already running in the background", nil
	}

	benchmarker := benchmark.New(c.client, c.cfg.BenchmarkTasks)
	if c.cfg.DefaultModel != "" {
		benchmarker.SetEvaluator(c.cfg.DefaultModel)
	}

	m.bgBenchmark = NewBackgroundBenchmark(ctx, benchmarker, c.cfg)
	m.bgBenchmark.Start()
	m.benchmarkDone = false

	return "✓ Started background benchmarking", nil
}

// ConfigCommand
type ConfigCommand struct{}

func NewConfigCommand() *ConfigCommand {
	return &ConfigCommand{}
}

func (c *ConfigCommand) Name() string {
	return "config"
}

func (c *ConfigCommand) Description() string {
	return "Show config file location"
}

func (c *ConfigCommand) Execute(ctx context.Context, args []string, m *chatModel) (string, error) {
	configPath, err := config.GetConfigPath()
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("Configuration file: %s\n\nEdit this file to customize:\n• System prompts\n• Benchmark tasks\n• Model capabilities\n• Tool call formats\n• Model-as-tools", configPath), nil
}

// ToolsCommand
type ToolsCommand struct {
	toolRegistry *tools.Registry
}

func NewToolsCommand(toolRegistry *tools.Registry) *ToolsCommand {
	return &ToolsCommand{toolRegistry: toolRegistry}
}

func (c *ToolsCommand) Name() string {
	return "tools"
}

func (c *ToolsCommand) Description() string {
	return "List available tools"
}

func (c *ToolsCommand) Execute(ctx context.Context, args []string, m *chatModel) (string, error) {
	var sb strings.Builder
	sb.WriteString("Available tools:\n\n")

	allTools := c.toolRegistry.All()
	for _, tool := range allTools {
		sb.WriteString(fmt.Sprintf("• **%s** - %s\n", tool.Name(), tool.Description()))
	}

	sb.WriteString(fmt.Sprintf("\nTotal: %d tools\n", len(allTools)))
	sb.WriteString("\nAdd model-as-tool in config.json:\n")
	sb.WriteString("```json\n")
	sb.WriteString(`"model_as_tools": [
  {
    "model_name": "qwen2.5-coder",
    "description": "Expert coding model for complex programming tasks",
    "enabled": true
  }
]`)
	sb.WriteString("\n```")

	return sb.String(), nil
}

// ClearQueueCommand
type ClearQueueCommand struct{}

func NewClearQueueCommand() *ClearQueueCommand {
	return &ClearQueueCommand{}
}

func (c *ClearQueueCommand) Name() string {
	return "clear-queue"
}

func (c *ClearQueueCommand) Description() string {
	return "Clear all queued messages"
}

func (c *ClearQueueCommand) Execute(ctx context.Context, args []string, m *chatModel) (string, error) {
	count := len(m.messageQueue)
	m.messageQueue = nil

	if count == 0 {
		return "Queue is already empty.", nil
	}

	return fmt.Sprintf("✓ Cleared %d queued message(s).", count), nil
}
