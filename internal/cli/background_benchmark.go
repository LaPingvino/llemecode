package cli

import (
	"context"
	"fmt"
	"sync"

	"github.com/LaPingvino/llemecode/internal/benchmark"
	"github.com/LaPingvino/llemecode/internal/config"
)

// BackgroundBenchmark runs benchmarks in the background and updates config when done
type BackgroundBenchmark struct {
	benchmarker   *benchmark.Benchmarker
	cfg           *config.Config
	ctx           context.Context
	cancel        context.CancelFunc
	done          chan struct{}
	mu            sync.Mutex
	running       bool
	progress      string
	partialScores map[string]*benchmark.ModelScore // Store partial results as we go
}

func NewBackgroundBenchmark(ctx context.Context, benchmarker *benchmark.Benchmarker, cfg *config.Config) *BackgroundBenchmark {
	ctx, cancel := context.WithCancel(ctx)
	return &BackgroundBenchmark{
		benchmarker:   benchmarker,
		cfg:           cfg,
		ctx:           ctx,
		cancel:        cancel,
		done:          make(chan struct{}),
		partialScores: make(map[string]*benchmark.ModelScore),
	}
}

func (bb *BackgroundBenchmark) Start() {
	bb.mu.Lock()
	if bb.running {
		bb.mu.Unlock()
		return
	}
	bb.running = true
	bb.mu.Unlock()

	go bb.run()
}

func (bb *BackgroundBenchmark) run() {
	defer close(bb.done)
	defer func() {
		bb.mu.Lock()
		bb.running = false
		bb.mu.Unlock()
	}()

	progressCh := make(chan string, 100)

	// Consume progress messages
	go func() {
		for msg := range progressCh {
			bb.mu.Lock()
			bb.progress = msg
			bb.mu.Unlock()
		}
	}()

	// Get list of models
	models, err := bb.benchmarker.ListModels(bb.ctx)
	if err != nil {
		close(progressCh)
		bb.mu.Lock()
		bb.progress = fmt.Sprintf("Failed to list models: %v", err)
		bb.mu.Unlock()
		return
	}

	progressCh <- fmt.Sprintf("Found %d models to benchmark", len(models))

	// Benchmark models one at a time with incremental saving
	allScores := make([]benchmark.ModelScore, 0, len(models))

	for _, model := range models {
		// Check for cancellation
		select {
		case <-bb.ctx.Done():
			progressCh <- "⚠ Benchmarking interrupted - saving partial results..."
			close(progressCh)
			bb.savePartialResults()
			bb.mu.Lock()
			bb.progress = fmt.Sprintf("✓ Partial results saved (%d/%d models)", len(allScores), len(models))
			bb.mu.Unlock()
			return
		default:
		}

		progressCh <- fmt.Sprintf("\n=== Benchmarking %s ===", model.Name)

		score, err := bb.benchmarker.BenchmarkModel(bb.ctx, model.Name, progressCh)
		if err != nil {
			progressCh <- fmt.Sprintf("Error benchmarking %s: %v", model.Name, err)
			continue
		}

		// Store partial score
		bb.mu.Lock()
		bb.partialScores[model.Name] = score
		bb.mu.Unlock()

		allScores = append(allScores, *score)

		// Save incrementally after each model
		bb.savePartialResults()
	}

	close(progressCh)

	// Final save with all results
	bb.benchmarker.UpdateConfig(bb.cfg, allScores)
	if err := bb.cfg.Save(); err != nil {
		bb.mu.Lock()
		bb.progress = fmt.Sprintf("Failed to save config: %v", err)
		bb.mu.Unlock()
		return
	}

	configDir, err := config.GetConfigDir()
	if err != nil {
		bb.mu.Lock()
		bb.progress = fmt.Sprintf("Failed to get config dir: %v", err)
		bb.mu.Unlock()
		return
	}

	resultsPath := configDir + "/benchmark_results.json"
	if err := bb.benchmarker.SaveResults(allScores, resultsPath); err != nil {
		bb.mu.Lock()
		bb.progress = fmt.Sprintf("Failed to save results: %v", err)
		bb.mu.Unlock()
		return
	}

	bb.mu.Lock()
	bb.progress = "✓ Background benchmarking complete!"
	bb.mu.Unlock()
}

func (bb *BackgroundBenchmark) savePartialResults() {
	bb.mu.Lock()
	defer bb.mu.Unlock()

	if len(bb.partialScores) == 0 {
		return
	}

	// Convert partial scores to slice
	scores := make([]benchmark.ModelScore, 0, len(bb.partialScores))
	for _, score := range bb.partialScores {
		scores = append(scores, *score)
	}

	// Update config with partial results
	bb.benchmarker.UpdateConfig(bb.cfg, scores)
	if err := bb.cfg.Save(); err != nil {
		bb.progress = fmt.Sprintf("Failed to save partial config: %v", err)
		return
	}

	// Save partial benchmark results
	configDir, err := config.GetConfigDir()
	if err != nil {
		bb.progress = fmt.Sprintf("Failed to get config dir: %v", err)
		return
	}

	resultsPath := configDir + "/benchmark_results_partial.json"
	if err := bb.benchmarker.SaveResults(scores, resultsPath); err != nil {
		bb.progress = fmt.Sprintf("Failed to save partial results: %v", err)
	}
}

func (bb *BackgroundBenchmark) IsRunning() bool {
	bb.mu.Lock()
	defer bb.mu.Unlock()
	return bb.running
}

func (bb *BackgroundBenchmark) GetProgress() string {
	bb.mu.Lock()
	defer bb.mu.Unlock()
	return bb.progress
}

func (bb *BackgroundBenchmark) Wait() {
	<-bb.done
}

func (bb *BackgroundBenchmark) Done() <-chan struct{} {
	return bb.done
}

func (bb *BackgroundBenchmark) Stop() {
	if bb.cancel != nil {
		bb.cancel()
	}
	bb.Wait()
}
