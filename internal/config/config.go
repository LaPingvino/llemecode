package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type Config struct {
	OllamaURL         string                     `json:"ollama_url"`
	DefaultModel      string                     `json:"default_model"`
	BenchmarkTasks    []BenchmarkTask            `json:"benchmark_tasks"`
	SystemPrompts     map[string]string          `json:"system_prompts"`
	ModelCapabilities map[string]ModelCapability `json:"model_capabilities"`
}

type BenchmarkTask struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Prompt      string `json:"prompt"`
	Category    string `json:"category"`
}

type ModelCapability struct {
	SupportsTools  bool     `json:"supports_tools"`
	ToolCallFormat string   `json:"tool_call_format"`
	MaxTokens      int      `json:"max_tokens,omitempty"`
	RecommendedFor []string `json:"recommended_for,omitempty"`
}

func GetConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}
	return filepath.Join(home, ".config", "llemecode"), nil
}

func GetConfigPath() (string, error) {
	dir, err := GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

func Load() (*Config, error) {
	configPath, err := GetConfigPath()
	if err != nil {
		return nil, err
	}

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		cfg := DefaultConfig()
		if err := cfg.Save(); err != nil {
			return nil, fmt.Errorf("save default config: %w", err)
		}
		return cfg, nil
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	return &cfg, nil
}

func (c *Config) Save() error {
	configPath, err := GetConfigPath()
	if err != nil {
		return err
	}

	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	return nil
}

func (c *Config) ModelSupportsTools(modelName string) bool {
	if cap, ok := c.ModelCapabilities[modelName]; ok {
		return cap.SupportsTools
	}
	// Default: assume tools are supported for unknown models
	return true
}

func (c *Config) GetToolCallFormat(modelName string) string {
	if cap, ok := c.ModelCapabilities[modelName]; ok {
		return cap.ToolCallFormat
	}
	return "native"
}

func DefaultConfig() *Config {
	return &Config{
		OllamaURL:    "http://localhost:11434",
		DefaultModel: "",
		BenchmarkTasks: []BenchmarkTask{
			{
				Name:        "code_generation",
				Description: "Generate a simple function",
				Prompt:      "Write a Python function that reverses a string. Only provide the code, no explanation.",
				Category:    "coding",
			},
			{
				Name:        "code_explanation",
				Description: "Explain code",
				Prompt:      "Explain what this does in 2-3 sentences: def fib(n): return n if n <= 1 else fib(n-1) + fib(n-2)",
				Category:    "coding",
			},
			{
				Name:        "reasoning",
				Description: "Logical reasoning",
				Prompt:      "If all roses are flowers and some flowers fade quickly, can we conclude that some roses fade quickly? Explain your reasoning briefly.",
				Category:    "reasoning",
			},
			{
				Name:        "tool_use",
				Description: "Understanding tool usage",
				Prompt:      "If you needed to check the weather in London, describe step-by-step what you would do.",
				Category:    "tool_use",
			},
			{
				Name:        "creative_writing",
				Description: "Creative writing ability",
				Prompt:      "Write a haiku about programming.",
				Category:    "creative",
			},
		},
		SystemPrompts: map[string]string{
			"default": `You are a helpful coding assistant with access to tools. Use tools when needed to help the user.`,

			"tool_xml": `You are a helpful coding assistant. When you need to use a tool, respond with XML tags like this:
<tool_call>
<name>tool_name</name>
<arguments>
{
  "arg1": "value1",
  "arg2": "value2"
}
</arguments>
</tool_call>

Available tools:
{{TOOLS}}

Use tools when appropriate to help answer questions. After the tool returns results, continue with your response.`,

			"tool_json": `You are a helpful coding assistant. When you need to use a tool, respond with a JSON block like this:
'''json
{
  "tool_call": {
    "name": "tool_name",
    "arguments": {
      "arg1": "value1",
      "arg2": "value2"
    }
  }
}
'''

Available tools:
{{TOOLS}}

Use tools when appropriate. After receiving tool results, provide your final answer.`,

			"tool_text": `You are a helpful coding assistant. When you need to use a tool, write it exactly like this:
USE_TOOL: tool_name
ARGS: {"arg1": "value1", "arg2": "value2"}

Available tools:
{{TOOLS}}

Use tools when needed to help answer the user's questions.`,
		},
		ModelCapabilities: make(map[string]ModelCapability),
	}
}
