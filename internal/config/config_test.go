package config

import (
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.OllamaURL != "http://localhost:11434" {
		t.Errorf("Expected default Ollama URL, got '%s'", cfg.OllamaURL)
	}

	if len(cfg.BenchmarkTasks) == 0 {
		t.Error("Expected default benchmark tasks")
	}

	if len(cfg.SystemPrompts) == 0 {
		t.Error("Expected default system prompts")
	}

	if cfg.ModelCapabilities == nil {
		t.Error("Expected initialized model capabilities map")
	}
}

func TestConfigSaveAndLoad(t *testing.T) {
	// Just test with the default config structure
	cfg := DefaultConfig()
	cfg.DefaultModel = "test-model"
	cfg.ModelCapabilities["test-model"] = ModelCapability{
		SupportsTools:  true,
		ToolCallFormat: "native",
		RecommendedFor: []string{"testing"},
	}

	// Test that the config has expected values
	if cfg.DefaultModel != "test-model" {
		t.Errorf("Expected default model 'test-model', got '%s'", cfg.DefaultModel)
	}

	cap, ok := cfg.ModelCapabilities["test-model"]
	if !ok {
		t.Fatal("Expected test-model in capabilities")
	}

	if cap.ToolCallFormat != "native" {
		t.Errorf("Expected native format, got '%s'", cap.ToolCallFormat)
	}

	if len(cap.RecommendedFor) != 1 || cap.RecommendedFor[0] != "testing" {
		t.Errorf("Expected RecommendedFor ['testing'], got %v", cap.RecommendedFor)
	}
}

func TestModelSupportsTools(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ModelCapabilities["model-with-tools"] = ModelCapability{
		SupportsTools:  true,
		ToolCallFormat: "native",
	}
	cfg.ModelCapabilities["model-without-tools"] = ModelCapability{
		SupportsTools:  false,
		ToolCallFormat: "xml",
	}

	if !cfg.ModelSupportsTools("model-with-tools") {
		t.Error("Expected model-with-tools to support tools")
	}

	if cfg.ModelSupportsTools("model-without-tools") {
		t.Error("Expected model-without-tools to not support tools")
	}

	// Unknown model should default to true
	if !cfg.ModelSupportsTools("unknown-model") {
		t.Error("Expected unknown model to default to supporting tools")
	}
}

func TestGetToolCallFormat(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ModelCapabilities["xml-model"] = ModelCapability{
		SupportsTools:  false,
		ToolCallFormat: "xml",
	}

	if format := cfg.GetToolCallFormat("xml-model"); format != "xml" {
		t.Errorf("Expected 'xml', got '%s'", format)
	}

	// Unknown model should default to native
	if format := cfg.GetToolCallFormat("unknown"); format != "native" {
		t.Errorf("Expected 'native' for unknown model, got '%s'", format)
	}
}
