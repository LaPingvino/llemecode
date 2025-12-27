package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/LaPingvino/llemecode/internal/config"
	"github.com/LaPingvino/llemecode/internal/ollama"
	"github.com/LaPingvino/llemecode/internal/tools"
)

type Agent struct {
	client         *ollama.Client
	toolRegistry   *tools.Registry
	config         *config.Config
	model          string
	messages       []ollama.Message
	toolCallFormat string
	disabledTools  []string // Combined list of disabled tools (config + session)
}

type Response struct {
	Content   string
	ToolCalls []ToolExecution
}

type ToolExecution struct {
	Name   string
	Args   map[string]interface{}
	Result string
	Error  error
}

func New(client *ollama.Client, toolRegistry *tools.Registry, cfg *config.Config, model string) *Agent {
	toolCallFormat := cfg.GetToolCallFormat(model)

	return &Agent{
		client:         client,
		toolRegistry:   toolRegistry,
		config:         cfg,
		model:          model,
		messages:       make([]ollama.Message, 0),
		toolCallFormat: toolCallFormat,
	}
}

func (a *Agent) AddSystemPrompt(customPrompt string) {
	var prompt string

	if customPrompt != "" {
		prompt = customPrompt
	} else {
		// Select appropriate system prompt based on tool call format
		switch a.toolCallFormat {
		case "native":
			prompt = a.config.SystemPrompts["default"]
		case "xml":
			prompt = a.config.SystemPrompts["tool_xml"]
		case "json":
			prompt = a.config.SystemPrompts["tool_json"]
		case "text":
			prompt = a.config.SystemPrompts["tool_text"]
		default:
			prompt = a.config.SystemPrompts["default"]
		}
	}

	// Inject tool descriptions for ALL formats
	// Even native models benefit from knowing what tools are available
	toolDesc := a.generateToolDescriptions()
	prompt = strings.Replace(prompt, "{{TOOLS}}", toolDesc, -1)

	a.messages = append(a.messages, ollama.Message{
		Role:    "system",
		Content: prompt,
	})
}

func (a *Agent) generateToolDescriptions() string {
	var sb strings.Builder
	for _, tool := range a.toolRegistry.AllFiltered(a.disabledTools) {
		sb.WriteString(fmt.Sprintf("\n- %s: %s\n", tool.Name(), tool.Description()))
		params, _ := json.MarshalIndent(tool.Parameters(), "  ", "  ")
		sb.WriteString(fmt.Sprintf("  Parameters: %s\n", string(params)))
	}
	return sb.String()
}

// SetDisabledTools updates the list of disabled tools for this agent
func (a *Agent) SetDisabledTools(disabledTools []string) {
	a.disabledTools = disabledTools
}

func (a *Agent) Chat(ctx context.Context, userMessage string) (*Response, error) {
	a.messages = append(a.messages, ollama.Message{
		Role:    "user",
		Content: userMessage,
	})

	maxIterations := 10
	var response Response

	for i := 0; i < maxIterations; i++ {
		chatResp, err := a.performChat(ctx)
		if err != nil {
			return nil, fmt.Errorf("chat request: %w", err)
		}

		a.messages = append(a.messages, chatResp.Message)

		// Parse tool calls based on format
		toolCalls := a.extractToolCalls(chatResp)

		if len(toolCalls) == 0 {
			// No tool calls - we're done
			// Collect the final response content (could be just text or text + reasoning about tool results)
			response.Content = chatResp.Message.Content
			return &response, nil
		}

		// Execute tool calls
		if err := a.executeToolCalls(ctx, toolCalls, &response); err != nil {
			return nil, err
		}

		// After executing tools, continue loop to let LLM respond with the results
		// The LLM will see the tool results and provide a final answer
	}

	return nil, fmt.Errorf("max iterations reached without completion")
}

func (a *Agent) performChat(ctx context.Context) (*ollama.ChatResponse, error) {
	req := ollama.ChatRequest{
		Model:    a.model,
		Messages: a.messages,
		Stream:   false,
	}

	// Add tools for native format only
	if a.toolCallFormat == "native" {
		ollamaTools := make([]ollama.Tool, 0)
		for _, tool := range a.toolRegistry.AllFiltered(a.disabledTools) {
			ollamaTools = append(ollamaTools, ollama.Tool{
				Type: "function",
				Function: ollama.ToolFunction{
					Name:        tool.Name(),
					Description: tool.Description(),
					Parameters:  tool.Parameters(),
				},
			})
		}
		req.Tools = ollamaTools
	}

	return a.client.Chat(ctx, req)
}

func (a *Agent) extractToolCalls(resp *ollama.ChatResponse) []ollama.ToolCall {
	// Native tool calls
	if len(resp.ToolCalls) > 0 {
		return resp.ToolCalls
	}

	// Parse fallback formats
	content := resp.Message.Content

	switch a.toolCallFormat {
	case "xml":
		return a.parseXMLToolCalls(content)
	case "json":
		return a.parseJSONToolCalls(content)
	case "text":
		return a.parseTextToolCalls(content)
	}

	return nil
}

func (a *Agent) parseXMLToolCalls(content string) []ollama.ToolCall {
	var toolCalls []ollama.ToolCall

	re := regexp.MustCompile(`(?s)<tool_call>(.*?)</tool_call>`)
	matches := re.FindAllStringSubmatch(content, -1)

	for _, match := range matches {
		if len(match) < 2 {
			continue
		}

		toolCallContent := match[1]

		// Extract name
		nameRe := regexp.MustCompile(`<name>(.*?)</name>`)
		nameMatch := nameRe.FindStringSubmatch(toolCallContent)
		if len(nameMatch) < 2 {
			continue
		}
		name := strings.TrimSpace(nameMatch[1])

		// Extract arguments
		argsRe := regexp.MustCompile(`(?s)<arguments>(.*?)</arguments>`)
		argsMatch := argsRe.FindStringSubmatch(toolCallContent)

		var args map[string]interface{}
		if len(argsMatch) >= 2 {
			argsJSON := strings.TrimSpace(argsMatch[1])
			json.Unmarshal([]byte(argsJSON), &args)
		}

		if args == nil {
			args = make(map[string]interface{})
		}

		toolCalls = append(toolCalls, ollama.ToolCall{
			Function: ollama.ToolCallFunction{
				Name:      name,
				Arguments: args,
			},
		})
	}

	return toolCalls
}

func (a *Agent) parseJSONToolCalls(content string) []ollama.ToolCall {
	var toolCalls []ollama.ToolCall

	// Look for ```json blocks
	re := regexp.MustCompile("(?s)```json\\s*\\n(.*?)\\n```")
	matches := re.FindAllStringSubmatch(content, -1)

	for _, match := range matches {
		if len(match) < 2 {
			continue
		}

		var data map[string]interface{}
		if err := json.Unmarshal([]byte(match[1]), &data); err != nil {
			continue
		}

		if toolCallData, ok := data["tool_call"].(map[string]interface{}); ok {
			name, _ := toolCallData["name"].(string)
			args, _ := toolCallData["arguments"].(map[string]interface{})

			if args == nil {
				args = make(map[string]interface{})
			}

			toolCalls = append(toolCalls, ollama.ToolCall{
				Function: ollama.ToolCallFunction{
					Name:      name,
					Arguments: args,
				},
			})
		}
	}

	return toolCalls
}

func (a *Agent) parseTextToolCalls(content string) []ollama.ToolCall {
	var toolCalls []ollama.ToolCall

	lines := strings.Split(content, "\n")
	var currentName string

	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])

		// Look for USE_TOOL: pattern
		if strings.HasPrefix(line, "USE_TOOL:") {
			currentName = strings.TrimSpace(strings.TrimPrefix(line, "USE_TOOL:"))

			// Look for ARGS: on next line
			if i+1 < len(lines) {
				nextLine := strings.TrimSpace(lines[i+1])
				if strings.HasPrefix(nextLine, "ARGS:") {
					argsJSON := strings.TrimSpace(strings.TrimPrefix(nextLine, "ARGS:"))

					var args map[string]interface{}
					json.Unmarshal([]byte(argsJSON), &args)

					if args == nil {
						args = make(map[string]interface{})
					}

					toolCalls = append(toolCalls, ollama.ToolCall{
						Function: ollama.ToolCallFunction{
							Name:      currentName,
							Arguments: args,
						},
					})

					i++ // Skip the ARGS line
				}
			}
		}
	}

	return toolCalls
}

func (a *Agent) executeToolCalls(ctx context.Context, toolCalls []ollama.ToolCall, response *Response) error {
	for _, toolCall := range toolCalls {
		execution := ToolExecution{
			Name: toolCall.Function.Name,
			Args: toolCall.Function.Arguments,
		}

		result, err := a.toolRegistry.Execute(ctx, toolCall.Function.Name, toolCall.Function.Arguments)
		execution.Result = result
		execution.Error = err

		response.ToolCalls = append(response.ToolCalls, execution)

		toolResultMsg := ollama.Message{
			Role:     "tool",
			ToolName: toolCall.Function.Name, // Required by Ollama API
		}

		if err != nil {
			toolResultMsg.Content = fmt.Sprintf("Error executing tool %s: %v", toolCall.Function.Name, err)
		} else {
			toolResultMsg.Content = result
		}

		a.messages = append(a.messages, toolResultMsg)
	}
	return nil
}

func (a *Agent) GetMessages() []ollama.Message {
	return a.messages
}

func (a *Agent) ClearHistory() {
	systemMsgs := make([]ollama.Message, 0)
	for _, msg := range a.messages {
		if msg.Role == "system" {
			systemMsgs = append(systemMsgs, msg)
		}
	}
	a.messages = systemMsgs
}

func FormatToolCall(tc ToolExecution) string {
	argsJSON, _ := json.MarshalIndent(tc.Args, "", "  ")
	result := fmt.Sprintf("🔧 Tool: %s\nArguments:\n%s\n", tc.Name, string(argsJSON))

	if tc.Error != nil {
		result += fmt.Sprintf("❌ Error: %v\n", tc.Error)
	} else {
		result += fmt.Sprintf("✅ Result:\n%s\n", tc.Result)
	}

	return result
}
