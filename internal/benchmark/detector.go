package benchmark

import (
	"context"
	"fmt"
	"strings"

	"github.com/LaPingvino/llemecode/internal/config"
	"github.com/LaPingvino/llemecode/internal/ollama"
)

type Detector struct {
	client *ollama.Client
}

func NewDetector(client *ollama.Client) *Detector {
	return &Detector{client: client}
}

// DetectCapabilities tests if a model supports native tool calling
func (d *Detector) DetectCapabilities(ctx context.Context, modelName string, progressChan chan<- string) config.ModelCapability {
	capability := config.ModelCapability{
		SupportsTools:  false,
		ToolCallFormat: "text", // default fallback
	}

	if progressChan != nil {
		progressChan <- fmt.Sprintf("Testing %s for native tool support...", modelName)
	}

	// Test 1: Try native tool calling
	if d.testNativeTools(ctx, modelName) {
		capability.SupportsTools = true
		capability.ToolCallFormat = "native"
		if progressChan != nil {
			progressChan <- fmt.Sprintf("✓ %s supports native tools", modelName)
		}
		return capability
	}

	if progressChan != nil {
		progressChan <- fmt.Sprintf("✗ %s doesn't support native tools, testing fallbacks...", modelName)
	}

	// Test 2: Try XML format
	if d.testXMLFormat(ctx, modelName) {
		capability.ToolCallFormat = "xml"
		if progressChan != nil {
			progressChan <- fmt.Sprintf("✓ %s works with XML format", modelName)
		}
		return capability
	}

	// Test 3: Try JSON format
	if d.testJSONFormat(ctx, modelName) {
		capability.ToolCallFormat = "json"
		if progressChan != nil {
			progressChan <- fmt.Sprintf("✓ %s works with JSON format", modelName)
		}
		return capability
	}

	// Default to text format
	if progressChan != nil {
		progressChan <- fmt.Sprintf("→ %s will use simple text format", modelName)
	}

	return capability
}

func (d *Detector) testNativeTools(ctx context.Context, modelName string) bool {
	testTool := ollama.Tool{
		Type: "function",
		Function: ollama.ToolFunction{
			Name:        "test_tool",
			Description: "A test tool",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"test": map[string]interface{}{
						"type":        "string",
						"description": "A test parameter",
					},
				},
			},
		},
	}

	resp, err := d.client.Chat(ctx, ollama.ChatRequest{
		Model: modelName,
		Messages: []ollama.Message{
			{Role: "user", Content: "Use the test_tool with test='hello'"},
		},
		Tools:  []ollama.Tool{testTool},
		Stream: false,
	})

	if err != nil {
		return false
	}

	return len(resp.Message.ToolCalls) > 0
}

func (d *Detector) testXMLFormat(ctx context.Context, modelName string) bool {
	prompt := `You have access to a test_tool. To use it, respond with:
<tool_call>
<name>test_tool</name>
<arguments>{"test": "hello"}</arguments>
</tool_call>

Now use the test_tool with test='hello'.`

	resp, err := d.client.Chat(ctx, ollama.ChatRequest{
		Model: modelName,
		Messages: []ollama.Message{
			{Role: "user", Content: prompt},
		},
		Stream: false,
	})

	if err != nil {
		return false
	}

	content := resp.Message.Content
	return strings.Contains(content, "<tool_call>") &&
		strings.Contains(content, "<name>test_tool</name>")
}

func (d *Detector) testJSONFormat(ctx context.Context, modelName string) bool {
	prompt := `You have access to a test_tool. To use it, respond with a JSON block:
'''json
{
  "tool_call": {
    "name": "test_tool",
    "arguments": {"test": "hello"}
  }
}
'''

Now use the test_tool with test='hello'.`

	resp, err := d.client.Chat(ctx, ollama.ChatRequest{
		Model: modelName,
		Messages: []ollama.Message{
			{Role: "user", Content: prompt},
		},
		Stream: false,
	})

	if err != nil {
		return false
	}

	content := resp.Message.Content
	return strings.Contains(content, "tool_call") &&
		strings.Contains(content, "test_tool")
}
