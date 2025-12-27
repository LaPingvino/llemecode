package tools

import (
	"context"
	"fmt"

	"github.com/LaPingvino/llemecode/internal/ollama"
)

// AskModelTool allows the LLM to invoke other specialized models
type AskModelTool struct {
	client      *ollama.Client
	modelName   string
	description string
}

func NewAskModelTool(client *ollama.Client, modelName, description string) *AskModelTool {
	return &AskModelTool{
		client:      client,
		modelName:   modelName,
		description: description,
	}
}

func (t *AskModelTool) Name() string {
	return fmt.Sprintf("ask_%s", t.modelName)
}

func (t *AskModelTool) Description() string {
	if t.description != "" {
		return t.description
	}
	return fmt.Sprintf("Ask the %s model a question. Use this when you need specialized help.", t.modelName)
}

func (t *AskModelTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"question": map[string]interface{}{
				"type":        "string",
				"description": "The question or prompt to send to the model",
			},
		},
		"required": []string{"question"},
	}
}

func (t *AskModelTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	question, ok := args["question"].(string)
	if !ok {
		return "", fmt.Errorf("question must be a string")
	}

	resp, err := t.client.Chat(ctx, ollama.ChatRequest{
		Model: t.modelName,
		Messages: []ollama.Message{
			{Role: "user", Content: question},
		},
		Stream: false,
	})
	if err != nil {
		return "", fmt.Errorf("ask %s: %w", t.modelName, err)
	}

	return resp.Message.Content, nil
}
