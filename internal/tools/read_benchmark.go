package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/LaPingvino/llemecode/internal/config"
)

type ReadBenchmarkTool struct{}

func NewReadBenchmarkTool() *ReadBenchmarkTool {
	return &ReadBenchmarkTool{}
}

func (t *ReadBenchmarkTool) Name() string {
	return "read_benchmark_results"
}

func (t *ReadBenchmarkTool) Description() string {
	return "Read benchmark results for all models to help make informed decisions about which model to use for specific tasks"
}

func (t *ReadBenchmarkTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}

func (t *ReadBenchmarkTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	configDir, err := config.GetConfigDir()
	if err != nil {
		return "", fmt.Errorf("get config dir: %w", err)
	}

	// Try full results first
	resultsPath := configDir + "/benchmark_results.json"
	content, err := os.ReadFile(resultsPath)
	if err != nil {
		// Try partial results
		partialPath := configDir + "/benchmark_results_partial.json"
		content, err = os.ReadFile(partialPath)
		if err != nil {
			return "", fmt.Errorf("no benchmark results found. Run /benchmark to generate them")
		}
		resultsPath = partialPath
	}

	// Parse the JSON to provide structured output
	var results []map[string]interface{}
	if err := json.Unmarshal(content, &results); err != nil {
		// If parsing fails, return raw content
		return string(content), nil
	}

	// Format for better readability
	output := fmt.Sprintf("Benchmark Results (from %s):\n\n", resultsPath)
	for _, result := range results {
		model := result["Model"]
		score := result["TotalScore"]
		rank := result["Rank"]
		latency := result["AvgLatency"]
		strengths, _ := result["Strengths"].([]interface{})
		description := result["Description"]

		output += fmt.Sprintf("Model: %v\n", model)
		output += fmt.Sprintf("  Rank: %v\n", rank)
		output += fmt.Sprintf("  Total Score: %.2f\n", score)
		output += fmt.Sprintf("  Avg Latency: %v\n", latency)

		if len(strengths) > 0 {
			output += "  Strengths: "
			for i, s := range strengths {
				if i > 0 {
					output += ", "
				}
				output += fmt.Sprintf("%v", s)
			}
			output += "\n"
		}

		if description != nil {
			output += fmt.Sprintf("  Description: %v\n", description)
		}

		output += "\n"
	}

	return output, nil
}
