package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
)

// MCPClient manages a connection to an MCP server
type MCPClient struct {
	serverName string
	command    string
	args       []string
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	stdout     io.ReadCloser
	stderr     io.ReadCloser
	reader     *bufio.Reader
	mu         sync.Mutex
	nextID     int
	tools      []MCPTool
}

// MCPTool represents a tool exposed by an MCP server
type MCPTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

// Request represents an MCP JSON-RPC request
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response represents an MCP JSON-RPC response
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

type RPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// NewMCPClient creates a new MCP client
func NewMCPClient(serverName, command string, args []string) *MCPClient {
	return &MCPClient{
		serverName: serverName,
		command:    command,
		args:       args,
		nextID:     1,
	}
}

// Start initializes the connection to the MCP server
func (c *MCPClient) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Start the MCP server process
	c.cmd = exec.CommandContext(ctx, c.command, c.args...)

	var err error
	c.stdin, err = c.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdin: %w", err)
	}

	c.stdout, err = c.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdout: %w", err)
	}

	c.stderr, err = c.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to get stderr: %w", err)
	}

	c.reader = bufio.NewReader(c.stdout)

	if err := c.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start MCP server: %w", err)
	}

	// Initialize the connection
	if err := c.initialize(); err != nil {
		c.Close()
		return fmt.Errorf("failed to initialize: %w", err)
	}

	// List available tools
	if err := c.listTools(); err != nil {
		c.Close()
		return fmt.Errorf("failed to list tools: %w", err)
	}

	return nil
}

// initialize sends the initialize request
func (c *MCPClient) initialize() error {
	req := Request{
		JSONRPC: "2.0",
		ID:      c.getNextID(),
		Method:  "initialize",
		Params:  json.RawMessage(`{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"llemecode","version":"0.1.0"}}`),
	}

	_, err := c.sendRequest(req)
	return err
}

// listTools retrieves the list of available tools
func (c *MCPClient) listTools() error {
	req := Request{
		JSONRPC: "2.0",
		ID:      c.getNextID(),
		Method:  "tools/list",
	}

	resp, err := c.sendRequest(req)
	if err != nil {
		return err
	}

	var result struct {
		Tools []MCPTool `json:"tools"`
	}

	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return fmt.Errorf("failed to parse tools list: %w", err)
	}

	c.tools = result.Tools
	return nil
}

// CallTool invokes a tool on the MCP server
func (c *MCPClient) CallTool(ctx context.Context, toolName string, arguments map[string]interface{}) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	params := map[string]interface{}{
		"name":      toolName,
		"arguments": arguments,
	}

	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return "", fmt.Errorf("failed to marshal params: %w", err)
	}

	req := Request{
		JSONRPC: "2.0",
		ID:      c.getNextID(),
		Method:  "tools/call",
		Params:  paramsJSON,
	}

	resp, err := c.sendRequest(req)
	if err != nil {
		return "", err
	}

	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}

	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return "", fmt.Errorf("failed to parse tool result: %w", err)
	}

	// Concatenate all text content
	var output string
	for _, content := range result.Content {
		if content.Type == "text" {
			output += content.Text
		}
	}

	return output, nil
}

// GetTools returns the list of available tools
func (c *MCPClient) GetTools() []MCPTool {
	c.mu.Lock()
	defer c.mu.Unlock()

	tools := make([]MCPTool, len(c.tools))
	copy(tools, c.tools)
	return tools
}

// sendRequest sends a request and waits for response
func (c *MCPClient) sendRequest(req Request) (*Response, error) {
	// Send request
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	data = append(data, '\n')
	if _, err := c.stdin.Write(data); err != nil {
		return nil, fmt.Errorf("failed to write request: %w", err)
	}

	// Read response
	line, err := c.reader.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var resp Response
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("MCP error %d: %s", resp.Error.Code, resp.Error.Message)
	}

	return &resp, nil
}

// getNextID returns the next request ID
func (c *MCPClient) getNextID() int {
	id := c.nextID
	c.nextID++
	return id
}

// Close terminates the connection to the MCP server
func (c *MCPClient) Close() error {
	if c.cmd != nil && c.cmd.Process != nil {
		c.stdin.Close()
		c.cmd.Process.Kill()
		c.cmd.Wait()
	}
	return nil
}

// ServerName returns the name of this MCP server
func (c *MCPClient) ServerName() string {
	return c.serverName
}
