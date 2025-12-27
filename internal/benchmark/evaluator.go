package benchmark

import (
	"context"
	"fmt"
	"strings"

	"github.com/LaPingvino/llemecode/internal/config"
	"github.com/LaPingvino/llemecode/internal/ollama"
)

type AIEvaluator struct {
	client         *ollama.Client
	evaluatorModel string
}

func NewAIEvaluator(client *ollama.Client, model string) *AIEvaluator {
	return &AIEvaluator{
		client:         client,
		evaluatorModel: model,
	}
}

// EvaluateResponse uses an LLM to evaluate another model's response
func (e *AIEvaluator) EvaluateResponse(ctx context.Context, task config.BenchmarkTask, response string) (float64, string, error) {
	prompt := fmt.Sprintf(`You are evaluating an LLM's response to a task. Rate the response on a scale of 0.0 to 1.0.

Task Category: %s
Task Description: %s
Task Prompt: %s

Model's Response:
%s

Evaluate this response based on:
- Correctness and accuracy
- Completeness
- Clarity and coherence
- Appropriateness for the task category

Respond in this exact format:
SCORE: [number between 0.0 and 1.0]
REASONING: [brief explanation]

Be strict but fair. Only exceptional responses should score above 0.9.`,
		task.Category, task.Description, task.Prompt, response)

	resp, err := e.client.Chat(ctx, ollama.ChatRequest{
		Model: e.evaluatorModel,
		Messages: []ollama.Message{
			{Role: "user", Content: prompt},
		},
		Stream: false,
	})
	if err != nil {
		return 0, "", fmt.Errorf("chat with evaluator: %w", err)
	}

	return e.parseEvaluation(resp.Message.Content)
}

func (e *AIEvaluator) parseEvaluation(content string) (float64, string, error) {
	lines := strings.Split(content, "\n")

	var score float64
	var reasoning string

	for _, line := range lines {
		line = strings.TrimSpace(line)

		if strings.HasPrefix(line, "SCORE:") {
			scoreStr := strings.TrimSpace(strings.TrimPrefix(line, "SCORE:"))
			fmt.Sscanf(scoreStr, "%f", &score)
		}

		if strings.HasPrefix(line, "REASONING:") {
			reasoning = strings.TrimSpace(strings.TrimPrefix(line, "REASONING:"))
		}
	}

	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}

	return score, reasoning, nil
}

// GenerateModelDescription uses an LLM to create a description based on strengths
func (e *AIEvaluator) GenerateModelDescription(ctx context.Context, score *ModelScore) (string, error) {
	strengthsStr := "general purpose"
	if len(score.Strengths) > 0 {
		strengthsStr = strings.Join(score.Strengths, ", ")
	}

	prompt := fmt.Sprintf(`Based on these benchmark results, write a concise one-sentence description of this model's best use cases.

Model: %s
Overall Score: %.2f
Strengths: %s
Average Latency: %v
Tool Support: %v

Write a single, clear sentence describing when to use this model. Be specific and practical.
Example format: "Fast general-purpose model, ideal for coding tasks and quick responses."`,
		score.Model, score.TotalScore, strengthsStr, score.AvgLatency, score.Capability.SupportsTools)

	resp, err := e.client.Chat(ctx, ollama.ChatRequest{
		Model: e.evaluatorModel,
		Messages: []ollama.Message{
			{Role: "user", Content: prompt},
		},
		Stream: false,
	})
	if err != nil {
		return "", fmt.Errorf("generate description: %w", err)
	}

	description := strings.TrimSpace(resp.Message.Content)
	// Remove quotes if present
	description = strings.Trim(description, "\"'")

	return description, nil
}
