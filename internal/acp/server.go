package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/LaPingvino/llemecode/internal/agent"
	"github.com/LaPingvino/llemecode/internal/config"
	"github.com/LaPingvino/llemecode/internal/ollama"
	"github.com/LaPingvino/llemecode/internal/tools"
)

// ACPServer implements the Anthropic Computer Protocol for Llemecode
type ACPServer struct {
	client       *ollama.Client
	config       *config.Config
	toolRegistry *tools.Registry
	agent        *agent.Agent
	reader       *bufio.Reader
	writer       io.Writer
}

// Request represents an ACP JSON-RPC request
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response represents an ACP JSON-RPC response
type Response struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *Error      `json:"error,omitempty"`
}

// Error represents a JSON-RPC error
type Error struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// ToolParams for tool execution
type ToolParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

// ChatParams for chat requests
type ChatParams struct {
	Message string `json:"message"`
	Model   string `json:"model,omitempty"`
}

func NewServer(client *ollama.Client, cfg *config.Config, toolRegistry *tools.Registry) *ACPServer {
	return &ACPServer{
		client:       client,
		config:       cfg,
		toolRegistry: toolRegistry,
		reader:       bufio.NewReader(os.Stdin),
		writer:       os.Stdout,
	}
}

// Start begins the ACP server loop
func (s *ACPServer) Start(ctx context.Context) error {
	// Initialize agent with default model
	model := s.config.DefaultModel
	if model == "" {
		return fmt.Errorf("no default model configured")
	}

	s.agent = agent.New(s.client, s.toolRegistry, s.config, model)
	s.agent.SetDisabledTools(s.config.DisabledTools)

	if sysPrompt, ok := s.config.SystemPrompts["default"]; ok {
		s.agent.AddSystemPrompt(sysPrompt)
	} else {
		s.agent.AddSystemPrompt("")
	}

	// Main request loop
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			if err := s.handleRequest(ctx); err != nil {
				if err == io.EOF {
					return nil
				}
				// Log error but continue
				fmt.Fprintf(os.Stderr, "Error handling request: %v\n", err)
			}
		}
	}
}

func (s *ACPServer) handleRequest(ctx context.Context) error {
	// Read line from stdin
	line, err := s.reader.ReadBytes('\n')
	if err != nil {
		return err
	}

	var req Request
	if err := json.Unmarshal(line, &req); err != nil {
		s.sendError(req.ID, -32700, "Parse error", err.Error())
		return nil
	}

	// Handle methods
	switch req.Method {
	case "initialize":
		s.handleInitialize(req)
	case "tools/list":
		s.handleToolsList(req)
	case "tools/call":
		s.handleToolCall(ctx, req)
	case "chat":
		s.handleChat(ctx, req)
	case "models/list":
		s.handleModelsList(ctx, req)
	case "models/switch":
		s.handleModelSwitch(req)
	default:
		s.sendError(req.ID, -32601, "Method not found", req.Method)
	}

	return nil
}

func (s *ACPServer) handleInitialize(req Request) {
	result := map[string]interface{}{
		"protocolVersion": "0.1.0",
		"serverInfo": map[string]interface{}{
			"name":    "llemecode",
			"version": "0.1.0",
		},
		"capabilities": map[string]interface{}{
			"tools": true,
			"chat":  true,
		},
	}
	s.sendResponse(req.ID, result)
}

func (s *ACPServer) handleToolsList(req Request) {
	allTools := s.toolRegistry.AllFiltered(s.config.DisabledTools)
	toolList := make([]map[string]interface{}, 0, len(allTools))

	for _, tool := range allTools {
		toolList = append(toolList, map[string]interface{}{
			"name":        tool.Name(),
			"description": tool.Description(),
			"inputSchema": tool.Parameters(),
		})
	}

	s.sendResponse(req.ID, map[string]interface{}{
		"tools": toolList,
	})
}

func (s *ACPServer) handleToolCall(ctx context.Context, req Request) {
	var params ToolParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.sendError(req.ID, -32602, "Invalid params", err.Error())
		return
	}

	result, err := s.toolRegistry.Execute(ctx, params.Name, params.Arguments)
	if err != nil {
		s.sendError(req.ID, -32000, "Tool execution failed", err.Error())
		return
	}

	s.sendResponse(req.ID, map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": result,
			},
		},
	})
}

func (s *ACPServer) handleChat(ctx context.Context, req Request) {
	var params ChatParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.sendError(req.ID, -32602, "Invalid params", err.Error())
		return
	}

	// Switch model if specified
	if params.Model != "" && params.Model != s.agent.GetMessages()[0].Role {
		s.agent = agent.New(s.client, s.toolRegistry, s.config, params.Model)
		s.agent.SetDisabledTools(s.config.DisabledTools)
		if sysPrompt, ok := s.config.SystemPrompts["default"]; ok {
			s.agent.AddSystemPrompt(sysPrompt)
		} else {
			s.agent.AddSystemPrompt("")
		}
	}

	resp, err := s.agent.Chat(ctx, params.Message)
	if err != nil {
		s.sendError(req.ID, -32000, "Chat failed", err.Error())
		return
	}

	// Format response with tool calls
	content := []map[string]interface{}{}

	// Add tool executions
	for _, tc := range resp.ToolCalls {
		content = append(content, map[string]interface{}{
			"type":  "tool_use",
			"name":  tc.Name,
			"input": tc.Args,
		})
		if tc.Error != nil {
			content = append(content, map[string]interface{}{
				"type":  "tool_result",
				"error": tc.Error.Error(),
			})
		} else {
			content = append(content, map[string]interface{}{
				"type": "tool_result",
				"text": tc.Result,
			})
		}
	}

	// Add final response
	if resp.Content != "" {
		content = append(content, map[string]interface{}{
			"type": "text",
			"text": resp.Content,
		})
	}

	s.sendResponse(req.ID, map[string]interface{}{
		"content": content,
	})
}

func (s *ACPServer) handleModelsList(ctx context.Context, req Request) {
	models, err := s.client.ListModels(ctx)
	if err != nil {
		s.sendError(req.ID, -32000, "Failed to list models", err.Error())
		return
	}

	modelList := make([]map[string]interface{}, 0, len(models))
	for _, model := range models {
		info := map[string]interface{}{
			"name": model.Name,
			"size": model.Size,
		}

		if cap, ok := s.config.ModelCapabilities[model.Name]; ok {
			info["supports_tools"] = cap.SupportsTools
			info["tool_format"] = cap.ToolCallFormat
			info["recommended_for"] = cap.RecommendedFor
		}

		modelList = append(modelList, info)
	}

	s.sendResponse(req.ID, map[string]interface{}{
		"models":        modelList,
		"default_model": s.config.DefaultModel,
	})
}

func (s *ACPServer) handleModelSwitch(req Request) {
	var params struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.sendError(req.ID, -32602, "Invalid params", err.Error())
		return
	}

	// Create new agent with new model
	s.agent = agent.New(s.client, s.toolRegistry, s.config, params.Model)
	s.agent.SetDisabledTools(s.config.DisabledTools)
	if sysPrompt, ok := s.config.SystemPrompts["default"]; ok {
		s.agent.AddSystemPrompt(sysPrompt)
	} else {
		s.agent.AddSystemPrompt("")
	}

	// Update default in config
	s.config.DefaultModel = params.Model
	if err := s.config.Save(); err != nil {
		s.sendError(req.ID, -32000, "Failed to save config", err.Error())
		return
	}

	s.sendResponse(req.ID, map[string]interface{}{
		"model": params.Model,
	})
}

func (s *ACPServer) sendResponse(id interface{}, result interface{}) {
	resp := Response{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
	s.send(resp)
}

func (s *ACPServer) sendError(id interface{}, code int, message string, data interface{}) {
	resp := Response{
		JSONRPC: "2.0",
		ID:      id,
		Error: &Error{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}
	s.send(resp)
}

func (s *ACPServer) send(resp Response) {
	data, err := json.Marshal(resp)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to marshal response: %v\n", err)
		return
	}
	data = append(data, '\n')
	s.writer.Write(data)
}
