package tools

import (
	"context"
	"fmt"
	"runtime"

	"github.com/LaPingvino/llemecode/internal/config"
	"github.com/LaPingvino/llemecode/internal/ollama"
)

// ConversationManager interface to avoid import cycle with agent
type ConversationManager interface {
	GetMessages() []ollama.Message
	ClearHistory()
}

// MemoryStatusTool reports current memory usage
type MemoryStatusTool struct{}

func NewMemoryStatusTool() *MemoryStatusTool {
	return &MemoryStatusTool{}
}

func (t *MemoryStatusTool) Name() string {
	return "check_memory_status"
}

func (t *MemoryStatusTool) Description() string {
	return "Check current memory usage and conversation size. Use this to determine if you need to compress the conversation history."
}

func (t *MemoryStatusTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}

func (t *MemoryStatusTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// Convert bytes to MB
	allocMB := float64(m.Alloc) / 1024 / 1024
	totalAllocMB := float64(m.TotalAlloc) / 1024 / 1024
	sysMB := float64(m.Sys) / 1024 / 1024

	status := fmt.Sprintf("Memory Status:\n")
	status += fmt.Sprintf("- Current Allocation: %.2f MB\n", allocMB)
	status += fmt.Sprintf("- Total Allocated (cumulative): %.2f MB\n", totalAllocMB)
	status += fmt.Sprintf("- System Memory: %.2f MB\n", sysMB)
	status += fmt.Sprintf("- Number of GC runs: %d\n", m.NumGC)

	// Memory pressure warning
	if allocMB > 500 {
		status += fmt.Sprintf("\n⚠️ WARNING: Memory usage is high (%.2f MB). Consider using compress_conversation to reduce memory usage.\n", allocMB)
	} else if allocMB > 200 {
		status += fmt.Sprintf("\nℹ️ Memory usage is moderate (%.2f MB). You may want to compress if the conversation gets much longer.\n", allocMB)
	} else {
		status += fmt.Sprintf("\n✓ Memory usage is normal (%.2f MB).\n", allocMB)
	}

	return status, nil
}

// CompressConversationTool compresses conversation history using an LLM
type CompressConversationTool struct {
	conversationMgr ConversationManager
	client          *ollama.Client
	config          *config.Config
}

func NewCompressConversationTool(conversationMgr ConversationManager, client *ollama.Client, cfg *config.Config) *CompressConversationTool {
	return &CompressConversationTool{
		conversationMgr: conversationMgr,
		client:          client,
		config:          cfg,
	}
}

func (t *CompressConversationTool) Name() string {
	return "compress_conversation"
}

func (t *CompressConversationTool) Description() string {
	return "Compress the conversation history into a concise summary, preserving important context while reducing memory usage. This creates a new conversation with the summary as context."
}

func (t *CompressConversationTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"preserve_recent": map[string]interface{}{
				"type":        "number",
				"description": "Number of recent messages to keep uncompressed (default: 5)",
			},
		},
	}
}

func (t *CompressConversationTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	preserveRecent := 5
	if pr, ok := args["preserve_recent"].(float64); ok {
		preserveRecent = int(pr)
	}

	messages := t.conversationMgr.GetMessages()
	if len(messages) <= preserveRecent+1 { // +1 for system prompt
		return "Conversation is too short to compress. No compression needed.", nil
	}

	// Extract messages to compress (excluding system prompt and recent messages)
	var toCompress []ollama.Message
	var toKeep []ollama.Message

	for i, msg := range messages {
		if i == 0 && msg.Role == "system" {
			// Skip system prompt
			continue
		}

		if i >= len(messages)-preserveRecent {
			toKeep = append(toKeep, msg)
		} else {
			toCompress = append(toCompress, msg)
		}
	}

	if len(toCompress) == 0 {
		return "No messages to compress.", nil
	}

	// Build compression prompt
	conversationText := ""
	for _, msg := range toCompress {
		conversationText += fmt.Sprintf("%s: %s\n\n", msg.Role, msg.Content)
	}

	compressionPrompt := fmt.Sprintf(`Compress the following conversation history into a concise summary that preserves:
1. Key facts and decisions made
2. Important context needed for future work
3. Current state of any ongoing tasks
4. Any code changes or file modifications made

Keep the summary under 500 words but ensure all critical information is retained.

Conversation to compress:
%s

Provide only the compressed summary, no additional commentary.`, conversationText)

	// Use the current model to compress
	resp, err := t.client.Chat(ctx, ollama.ChatRequest{
		Model: t.config.DefaultModel,
		Messages: []ollama.Message{
			{Role: "user", Content: compressionPrompt},
		},
		Stream: false,
	})
	if err != nil {
		return "", fmt.Errorf("compression failed: %w", err)
	}

	summary := resp.Message.Content

	// Clear conversation and rebuild with summary + recent messages
	t.conversationMgr.ClearHistory()

	// Note: The actual rebuilding of messages needs to be handled by the caller
	// This tool returns instructions for what was compressed

	// Force garbage collection
	runtime.GC()

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	allocMB := float64(m.Alloc) / 1024 / 1024

	result := fmt.Sprintf("✓ Conversation has been cleared for compression.\n\n")
	result += fmt.Sprintf("📋 Compressed Summary (from %d messages):\n%s\n\n", len(toCompress), summary)
	result += fmt.Sprintf("Note: You should now restart with this summary as context.\n")
	result += fmt.Sprintf("- Current memory usage: %.2f MB\n", allocMB)

	return result, nil
}

// GetConversationSizeTool reports conversation statistics
type GetConversationSizeTool struct {
	conversationMgr ConversationManager
}

func NewGetConversationSizeTool(conversationMgr ConversationManager) *GetConversationSizeTool {
	return &GetConversationSizeTool{conversationMgr: conversationMgr}
}

func (t *GetConversationSizeTool) Name() string {
	return "get_conversation_size"
}

func (t *GetConversationSizeTool) Description() string {
	return "Get statistics about the current conversation including message count and estimated token usage."
}

func (t *GetConversationSizeTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}

func (t *GetConversationSizeTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	messages := t.conversationMgr.GetMessages()

	totalChars := 0
	for _, msg := range messages {
		totalChars += len(msg.Content)
	}

	// Rough token estimate (1 token ≈ 4 characters)
	estimatedTokens := totalChars / 4

	result := fmt.Sprintf("Conversation Statistics:\n")
	result += fmt.Sprintf("- Total messages: %d\n", len(messages))
	result += fmt.Sprintf("- Total characters: %d\n", totalChars)
	result += fmt.Sprintf("- Estimated tokens: ~%d\n", estimatedTokens)

	if estimatedTokens > 8000 {
		result += fmt.Sprintf("\n⚠️ WARNING: Conversation is getting very long (%d tokens). Consider using compress_conversation.\n", estimatedTokens)
	} else if estimatedTokens > 4000 {
		result += fmt.Sprintf("\nℹ️ Conversation is moderately long (%d tokens).\n", estimatedTokens)
	} else {
		result += fmt.Sprintf("\n✓ Conversation size is manageable (%d tokens).\n", estimatedTokens)
	}

	return result, nil
}
