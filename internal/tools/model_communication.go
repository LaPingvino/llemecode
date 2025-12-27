package tools

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// MessageChannel allows sub-models to communicate back to the main LLM
type MessageChannel struct {
	mu       sync.RWMutex
	messages []ChannelMessage
}

type ChannelMessage struct {
	FromModel string    `json:"from_model"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
	Priority  string    `json:"priority"` // "info", "warning", "error"
}

func NewMessageChannel() *MessageChannel {
	return &MessageChannel{
		messages: make([]ChannelMessage, 0),
	}
}

// SendMessage adds a message to the channel
func (mc *MessageChannel) SendMessage(fromModel, message, priority string) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	mc.messages = append(mc.messages, ChannelMessage{
		FromModel: fromModel,
		Message:   message,
		Timestamp: time.Now(),
		Priority:  priority,
	})
}

// GetMessages retrieves all messages and optionally clears them
func (mc *MessageChannel) GetMessages(clear bool) []ChannelMessage {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	messages := make([]ChannelMessage, len(mc.messages))
	copy(messages, mc.messages)

	if clear {
		mc.messages = make([]ChannelMessage, 0)
	}

	return messages
}

// HasMessages checks if there are pending messages
func (mc *MessageChannel) HasMessages() bool {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	return len(mc.messages) > 0
}

// SendMessageTool allows models to send messages to the main LLM
type SendMessageTool struct {
	channel   *MessageChannel
	modelName string
}

func NewSendMessageTool(channel *MessageChannel, modelName string) *SendMessageTool {
	return &SendMessageTool{
		channel:   channel,
		modelName: modelName,
	}
}

func (t *SendMessageTool) Name() string {
	return "send_message_to_main"
}

func (t *SendMessageTool) Description() string {
	return "Send a message back to the main LLM. Use this to report progress, findings, warnings, or request clarification when running as a sub-model."
}

func (t *SendMessageTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"message": map[string]interface{}{
				"type":        "string",
				"description": "The message to send to the main LLM",
			},
			"priority": map[string]interface{}{
				"type":        "string",
				"description": "Priority level: 'info', 'warning', or 'error' (default: 'info')",
				"enum":        []string{"info", "warning", "error"},
			},
		},
		"required": []string{"message"},
	}
}

func (t *SendMessageTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	message, ok := args["message"].(string)
	if !ok {
		return "", fmt.Errorf("message must be a string")
	}

	priority := "info"
	if p, ok := args["priority"].(string); ok {
		priority = p
	}

	t.channel.SendMessage(t.modelName, message, priority)

	return fmt.Sprintf("✓ Message sent to main LLM"), nil
}

// ReceiveMessagesTool allows the main LLM to check for messages from sub-models
type ReceiveMessagesTool struct {
	channel *MessageChannel
}

func NewReceiveMessagesTool(channel *MessageChannel) *ReceiveMessagesTool {
	return &ReceiveMessagesTool{channel: channel}
}

func (t *ReceiveMessagesTool) Name() string {
	return "check_messages_from_submodels"
}

func (t *ReceiveMessagesTool) Description() string {
	return "Check for messages from sub-models (model-as-tool invocations). Use this to see if any background models have sent updates, warnings, or need clarification."
}

func (t *ReceiveMessagesTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"clear": map[string]interface{}{
				"type":        "boolean",
				"description": "Clear messages after reading (default: true)",
			},
		},
	}
}

func (t *ReceiveMessagesTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	clear := true
	if c, ok := args["clear"].(bool); ok {
		clear = c
	}

	messages := t.channel.GetMessages(clear)

	if len(messages) == 0 {
		return "No messages from sub-models.", nil
	}

	result := fmt.Sprintf("Messages from sub-models (%d):\n\n", len(messages))

	for i, msg := range messages {
		emoji := "ℹ️"
		if msg.Priority == "warning" {
			emoji = "⚠️"
		} else if msg.Priority == "error" {
			emoji = "❌"
		}

		timeSince := time.Since(msg.Timestamp)
		result += fmt.Sprintf("%d. %s [%s] (%s ago)\n", i+1, emoji, msg.FromModel, formatDuration(timeSince))
		result += fmt.Sprintf("   %s\n\n", msg.Message)
	}

	if clear {
		result += "(Messages have been cleared)\n"
	}

	return result, nil
}

// Enhanced AskModelTool with communication channel
type AskModelToolWithComm struct {
	*AskModelTool
	channel *MessageChannel
}

func NewAskModelToolWithComm(base *AskModelTool, channel *MessageChannel) *AskModelToolWithComm {
	return &AskModelToolWithComm{
		AskModelTool: base,
		channel:      channel,
	}
}

func (t *AskModelToolWithComm) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	// Execute the base tool
	result, err := t.AskModelTool.Execute(ctx, args)

	// Check for any messages that were sent during execution
	if t.channel.HasMessages() {
		messages := t.channel.GetMessages(false) // Don't clear, let main LLM check them
		if len(messages) > 0 {
			lastMsg := messages[len(messages)-1]
			if lastMsg.FromModel == t.AskModelTool.modelName {
				result += fmt.Sprintf("\n\n[Note: Sub-model sent a message - use check_messages_from_submodels to read it]")
			}
		}
	}

	return result, err
}
