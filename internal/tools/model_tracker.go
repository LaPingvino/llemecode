package tools

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"
)

// ModelMemoryTracker tracks memory usage per model
type ModelMemoryTracker struct {
	mu          sync.RWMutex
	modelStats  map[string]*ModelStats
	lastGC      time.Time
	gcThreshold float64 // MB threshold before suggesting GC
}

type ModelStats struct {
	ModelName      string
	LastUsed       time.Time
	UseCount       int64
	TotalTokens    int64
	EstimatedMemMB float64
	Active         bool
}

func NewModelMemoryTracker() *ModelMemoryTracker {
	return &ModelMemoryTracker{
		modelStats:  make(map[string]*ModelStats),
		lastGC:      time.Now(),
		gcThreshold: 400, // Suggest GC when total memory > 400MB
	}
}

// RecordModelUse records when a model is used
func (t *ModelMemoryTracker) RecordModelUse(modelName string, tokenCount int64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if _, exists := t.modelStats[modelName]; !exists {
		t.modelStats[modelName] = &ModelStats{
			ModelName: modelName,
		}
	}

	stats := t.modelStats[modelName]
	stats.LastUsed = time.Now()
	stats.UseCount++
	stats.TotalTokens += tokenCount
	stats.Active = true

	// Rough estimation: 1MB per 1000 tokens (conservative)
	stats.EstimatedMemMB = float64(stats.TotalTokens) / 1000.0
}

// MarkModelInactive marks a model as inactive (ready for GC)
func (t *ModelMemoryTracker) MarkModelInactive(modelName string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if stats, exists := t.modelStats[modelName]; exists {
		stats.Active = false
	}
}

// GetModelStats returns stats for a specific model
func (t *ModelMemoryTracker) GetModelStats(modelName string) *ModelStats {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if stats, exists := t.modelStats[modelName]; exists {
		return &ModelStats{
			ModelName:      stats.ModelName,
			LastUsed:       stats.LastUsed,
			UseCount:       stats.UseCount,
			TotalTokens:    stats.TotalTokens,
			EstimatedMemMB: stats.EstimatedMemMB,
			Active:         stats.Active,
		}
	}
	return nil
}

// GetAllStats returns all model statistics
func (t *ModelMemoryTracker) GetAllStats() []*ModelStats {
	t.mu.RLock()
	defer t.mu.RUnlock()

	stats := make([]*ModelStats, 0, len(t.modelStats))
	for _, s := range t.modelStats {
		stats = append(stats, &ModelStats{
			ModelName:      s.ModelName,
			LastUsed:       s.LastUsed,
			UseCount:       s.UseCount,
			TotalTokens:    s.TotalTokens,
			EstimatedMemMB: s.EstimatedMemMB,
			Active:         s.Active,
		})
	}
	return stats
}

// GetInactiveModels returns models that haven't been used recently
func (t *ModelMemoryTracker) GetInactiveModels(inactiveDuration time.Duration) []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	inactive := []string{}
	now := time.Now()

	for modelName, stats := range t.modelStats {
		if !stats.Active && now.Sub(stats.LastUsed) > inactiveDuration {
			inactive = append(inactive, modelName)
		}
	}

	return inactive
}

// ShouldGarbageCollect checks if GC is recommended
func (t *ModelMemoryTracker) ShouldGarbageCollect() bool {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	memMB := float64(m.Alloc) / 1024 / 1024

	return memMB > t.gcThreshold && time.Since(t.lastGC) > 5*time.Minute
}

// PerformGarbageCollection runs GC and cleans up inactive model stats
func (t *ModelMemoryTracker) PerformGarbageCollection(inactiveDuration time.Duration) (freedMB float64, removed []string) {
	// Get memory before GC
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	beforeMB := float64(before.Alloc) / 1024 / 1024

	// Remove inactive model stats
	inactive := t.GetInactiveModels(inactiveDuration)

	t.mu.Lock()
	for _, modelName := range inactive {
		delete(t.modelStats, modelName)
		removed = append(removed, modelName)
	}
	t.mu.Unlock()

	// Run garbage collection
	runtime.GC()
	t.lastGC = time.Now()

	// Get memory after GC
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	afterMB := float64(after.Alloc) / 1024 / 1024

	freedMB = beforeMB - afterMB
	return freedMB, removed
}

// GetMemoryReport generates a formatted memory report
func (t *ModelMemoryTracker) GetMemoryReport() string {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	totalMemMB := float64(m.Alloc) / 1024 / 1024

	stats := t.GetAllStats()

	report := fmt.Sprintf("Memory Report:\n")
	report += fmt.Sprintf("Total System Memory: %.2f MB\n\n", totalMemMB)

	if len(stats) == 0 {
		report += "No model usage tracked yet.\n"
		return report
	}

	report += "Model Usage:\n"
	for _, s := range stats {
		status := "💤 Inactive"
		if s.Active {
			status = "✓ Active"
		}

		timeSince := time.Since(s.LastUsed)
		report += fmt.Sprintf("  %s %s\n", status, s.ModelName)
		report += fmt.Sprintf("    Last used: %s ago\n", formatDuration(timeSince))
		report += fmt.Sprintf("    Use count: %d\n", s.UseCount)
		report += fmt.Sprintf("    Estimated memory: %.2f MB\n", s.EstimatedMemMB)
		report += "\n"
	}

	if t.ShouldGarbageCollect() {
		report += "⚠️ Memory usage is high. Consider running garbage collection.\n"
	}

	return report
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	} else if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	} else if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

// ModelMemoryReportTool provides memory tracking information to the LLM
type ModelMemoryReportTool struct {
	tracker *ModelMemoryTracker
}

func NewModelMemoryReportTool(tracker *ModelMemoryTracker) *ModelMemoryReportTool {
	return &ModelMemoryReportTool{tracker: tracker}
}

func (t *ModelMemoryReportTool) Name() string {
	return "get_model_memory_report"
}

func (t *ModelMemoryReportTool) Description() string {
	return "Get detailed memory usage report for all models, showing which models are using the most memory and which are inactive."
}

func (t *ModelMemoryReportTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}

func (t *ModelMemoryReportTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	return t.tracker.GetMemoryReport(), nil
}

// GarbageCollectModelsTool allows the LLM to trigger garbage collection
type GarbageCollectModelsTool struct {
	tracker *ModelMemoryTracker
}

func NewGarbageCollectModelsTool(tracker *ModelMemoryTracker) *GarbageCollectModelsTool {
	return &GarbageCollectModelsTool{tracker: tracker}
}

func (t *GarbageCollectModelsTool) Name() string {
	return "garbage_collect_models"
}

func (t *GarbageCollectModelsTool) Description() string {
	return "Perform garbage collection to free up memory from inactive models. Use this when memory usage is high."
}

func (t *GarbageCollectModelsTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"inactive_minutes": map[string]interface{}{
				"type":        "number",
				"description": "Models inactive for this many minutes will be garbage collected (default: 10)",
			},
		},
	}
}

func (t *GarbageCollectModelsTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	inactiveMinutes := 10.0
	if im, ok := args["inactive_minutes"].(float64); ok {
		inactiveMinutes = im
	}

	freedMB, removed := t.tracker.PerformGarbageCollection(time.Duration(inactiveMinutes) * time.Minute)

	result := fmt.Sprintf("✓ Garbage collection complete!\n")
	result += fmt.Sprintf("- Freed: %.2f MB\n", freedMB)

	if len(removed) > 0 {
		result += fmt.Sprintf("- Removed inactive models: %v\n", removed)
	} else {
		result += "- No inactive models to remove\n"
	}

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	result += fmt.Sprintf("- Current memory: %.2f MB\n", float64(m.Alloc)/1024/1024)

	return result, nil
}
