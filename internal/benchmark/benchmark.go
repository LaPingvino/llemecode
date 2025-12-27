package benchmark

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/LaPingvino/llemecode/internal/config"
	"github.com/LaPingvino/llemecode/internal/ollama"
)

type ModelScore struct {
	Model       string
	TotalScore  float64
	Scores      map[string]float64
	AvgLatency  time.Duration
	Strengths   []string
	Description string
	Capability  config.ModelCapability
	Rank        int
}

type Benchmarker struct {
	client    *ollama.Client
	detector  *Detector
	evaluator *AIEvaluator
	tasks     []config.BenchmarkTask
}

func New(client *ollama.Client, tasks []config.BenchmarkTask) *Benchmarker {
	if len(tasks) == 0 {
		tasks = getDefaultTasks()
	}

	return &Benchmarker{
		client:   client,
		detector: NewDetector(client),
		tasks:    tasks,
	}
}

func (b *Benchmarker) SetEvaluator(evaluatorModel string) {
	if evaluatorModel != "" {
		b.evaluator = NewAIEvaluator(b.client, evaluatorModel)
	}
}

func (b *Benchmarker) BenchmarkModel(ctx context.Context, modelName string, progressChan chan<- string) (*ModelScore, error) {
	score := &ModelScore{
		Model:  modelName,
		Scores: make(map[string]float64),
	}

	// Detect capabilities first
	score.Capability = b.detector.DetectCapabilities(ctx, modelName, progressChan)

	totalLatency := time.Duration(0)
	categoryScores := make(map[string][]float64)

	for _, task := range b.tasks {
		if progressChan != nil {
			progressChan <- fmt.Sprintf("Running '%s' test on %s", task.Name, modelName)
		}

		start := time.Now()
		resp, err := b.client.Chat(ctx, ollama.ChatRequest{
			Model: modelName,
			Messages: []ollama.Message{
				{Role: "user", Content: task.Prompt},
			},
			Stream: false,
		})
		latency := time.Since(start)
		totalLatency += latency

		if err != nil {
			score.Scores[task.Name] = 0
			if progressChan != nil {
				progressChan <- fmt.Sprintf("  ✗ Failed: %v", err)
			}
			continue
		}

		var taskScore float64
		if b.evaluator != nil {
			// Use AI evaluator
			aiScore, reasoning, err := b.evaluator.EvaluateResponse(ctx, task, resp.Message.Content)
			if err != nil {
				if progressChan != nil {
					progressChan <- fmt.Sprintf("  ⚠ Evaluation failed, using fallback: %v", err)
				}
				taskScore = evaluateResponse(task, resp.Message.Content, latency)
			} else {
				taskScore = aiScore
				if progressChan != nil {
					progressChan <- fmt.Sprintf("  Score: %.2f - %s", taskScore, reasoning)
				}
			}
		} else {
			// Use simple heuristic evaluation
			taskScore = evaluateResponse(task, resp.Message.Content, latency)
			if progressChan != nil {
				progressChan <- fmt.Sprintf("  Score: %.2f", taskScore)
			}
		}

		score.Scores[task.Name] = taskScore
		categoryScores[task.Category] = append(categoryScores[task.Category], taskScore)
	}

	// Determine strengths
	for category, scores := range categoryScores {
		avg := average(scores)
		if avg > 0.7 {
			score.Strengths = append(score.Strengths, category)
		}
	}

	score.TotalScore = average(mapToSlice(score.Scores))
	score.AvgLatency = totalLatency / time.Duration(len(b.tasks))

	// Generate description using AI if evaluator is available
	if b.evaluator != nil {
		if progressChan != nil {
			progressChan <- fmt.Sprintf("Generating AI description for %s...", modelName)
		}
		desc, err := b.evaluator.GenerateModelDescription(ctx, score)
		if err == nil {
			score.Description = desc
		} else {
			if progressChan != nil {
				progressChan <- fmt.Sprintf("  ⚠ Description generation failed: %v", err)
			}
			score.Description = generateDescription(score)
		}
	} else {
		score.Description = generateDescription(score)
	}

	return score, nil
}

func (b *Benchmarker) BenchmarkAll(ctx context.Context, progressChan chan<- string) ([]ModelScore, error) {
	models, err := b.client.ListModels(ctx)
	if err != nil {
		return nil, fmt.Errorf("list models: %w", err)
	}

	if progressChan != nil {
		progressChan <- fmt.Sprintf("Found %d models to benchmark", len(models))
	}

	scores := make([]ModelScore, 0, len(models))
	for _, model := range models {
		if progressChan != nil {
			progressChan <- fmt.Sprintf("\n=== Benchmarking %s ===", model.Name)
		}

		score, err := b.BenchmarkModel(ctx, model.Name, progressChan)
		if err != nil {
			if progressChan != nil {
				progressChan <- fmt.Sprintf("Error benchmarking %s: %v", model.Name, err)
			}
			continue
		}
		scores = append(scores, *score)
	}

	// Sort by total score
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].TotalScore > scores[j].TotalScore
	})

	// Assign ranks
	for i := range scores {
		scores[i].Rank = i + 1
	}

	return scores, nil
}

func (b *Benchmarker) SelectBestModel(scores []ModelScore) string {
	if len(scores) == 0 {
		return ""
	}

	// Prefer models with native tool support and good scores
	for _, score := range scores {
		if score.Capability.SupportsTools && score.TotalScore > 0.6 {
			return score.Model
		}
	}

	// Otherwise just pick the highest scoring
	return scores[0].Model
}

func (b *Benchmarker) SaveResults(scores []ModelScore, outputPath string) error {
	data, err := json.MarshalIndent(scores, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal results: %w", err)
	}

	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	if err := os.WriteFile(outputPath, data, 0644); err != nil {
		return fmt.Errorf("write results: %w", err)
	}

	return nil
}

func (b *Benchmarker) UpdateConfig(cfg *config.Config, scores []ModelScore) {
	// Update model capabilities
	for _, score := range scores {
		capability := score.Capability
		capability.RecommendedFor = score.Strengths
		cfg.ModelCapabilities[score.Model] = capability
	}

	// Set default model if not already set
	if cfg.DefaultModel == "" {
		cfg.DefaultModel = b.SelectBestModel(scores)
	}
}

func evaluateResponse(task config.BenchmarkTask, response string, latency time.Duration) float64 {
	score := 0.0

	// Basic length check
	if len(response) > 50 {
		score += 0.3
	}

	if len(response) > 200 {
		score += 0.2
	}

	// Latency score
	if latency < 5*time.Second {
		score += 0.3
	} else if latency < 10*time.Second {
		score += 0.2
	} else if latency < 20*time.Second {
		score += 0.1
	}

	// Base score for completing
	score += 0.2

	return score
}

func generateDescription(score *ModelScore) string {
	if len(score.Strengths) == 0 {
		return "General purpose model"
	}

	desc := "Good for: " + score.Strengths[0]
	for i := 1; i < len(score.Strengths) && i < 3; i++ {
		desc += ", " + score.Strengths[i]
	}

	return desc
}

func average(nums []float64) float64 {
	if len(nums) == 0 {
		return 0
	}
	sum := 0.0
	for _, n := range nums {
		sum += n
	}
	return sum / float64(len(nums))
}

func mapToSlice(m map[string]float64) []float64 {
	slice := make([]float64, 0, len(m))
	for _, v := range m {
		slice = append(slice, v)
	}
	return slice
}

func getDefaultTasks() []config.BenchmarkTask {
	return []config.BenchmarkTask{
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
			Prompt:      "If all roses are flowers and some flowers fade quickly, can we conclude that some roses fade quickly? Explain briefly.",
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
	}
}
