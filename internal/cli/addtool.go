package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/LaPingvino/llemecode/internal/config"
	"github.com/LaPingvino/llemecode/internal/ollama"
	"github.com/LaPingvino/llemecode/internal/tools"
)

// AddToolCommand allows enabling models as tools dynamically
type AddToolCommand struct {
	client       *ollama.Client
	cfg          *config.Config
	toolRegistry *tools.Registry
}

func NewAddToolCommand(client *ollama.Client, cfg *config.Config, toolRegistry *tools.Registry) *AddToolCommand {
	return &AddToolCommand{client: client, cfg: cfg, toolRegistry: toolRegistry}
}

func (c *AddToolCommand) Name() string {
	return "addtool"
}

func (c *AddToolCommand) Description() string {
	return "Enable a model as a tool (usage: /addtool <model-name> [description])"
}

func (c *AddToolCommand) Execute(ctx context.Context, args []string, m *chatModel) (string, error) {
	if len(args) == 0 {
		// Show available models
		models, err := c.client.ListModels(ctx)
		if err != nil {
			return "", fmt.Errorf("failed to list models: %w", err)
		}

		var sb strings.Builder
		sb.WriteString("Available models to add as tools:\n\n")

		// Filter out current model and already-enabled tools
		currentModel := c.cfg.DefaultModel
		enabledTools := make(map[string]bool)
		for _, mat := range c.cfg.ModelAsTools {
			if mat.Enabled {
				enabledTools[mat.ModelName] = true
			}
		}

		for _, model := range models {
			if model.Name == currentModel {
				continue // Don't add current model as tool
			}

			if enabledTools[model.Name] {
				sb.WriteString(fmt.Sprintf("✓ %s (already enabled)\n", model.Name))
			} else {
				sb.WriteString(fmt.Sprintf("  %s", model.Name))
				if cap, ok := c.cfg.ModelCapabilities[model.Name]; ok && len(cap.RecommendedFor) > 0 {
					sb.WriteString(fmt.Sprintf(" - good for: %s", strings.Join(cap.RecommendedFor, ", ")))
				}
				sb.WriteString("\n")
			}
		}

		sb.WriteString("\nUsage: /addtool <model-name> [description]\n")
		sb.WriteString("Example: /addtool qwen2.5-coder Expert at complex coding tasks")

		return sb.String(), nil
	}

	modelName := args[0]

	// Verify model exists
	models, err := c.client.ListModels(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to verify model: %w", err)
	}

	found := false
	for _, model := range models {
		if model.Name == modelName {
			found = true
			break
		}
	}

	if !found {
		return "", fmt.Errorf("model '%s' not found. Use /addtool to see available models", modelName)
	}

	// Get description (optional)
	description := ""
	if len(args) > 1 {
		description = strings.Join(args[1:], " ")
	} else {
		// Auto-generate description from capabilities
		if cap, ok := c.cfg.ModelCapabilities[modelName]; ok && len(cap.RecommendedFor) > 0 {
			description = fmt.Sprintf("Specialized in: %s", strings.Join(cap.RecommendedFor, ", "))
		} else {
			description = fmt.Sprintf("Ask the %s model for help", modelName)
		}
	}

	// Check if already enabled
	for _, mat := range c.cfg.ModelAsTools {
		if mat.ModelName == modelName && mat.Enabled {
			return fmt.Sprintf("Model '%s' is already enabled as a tool", modelName), nil
		}
	}

	// Add to config
	c.cfg.ModelAsTools = append(c.cfg.ModelAsTools, config.ModelAsTool{
		ModelName:   modelName,
		Description: description,
		Enabled:     true,
	})

	if err := c.cfg.Save(); err != nil {
		return "", fmt.Errorf("failed to save config: %w", err)
	}

	// Register the tool immediately
	c.toolRegistry.Register(tools.NewAskModelTool(c.client, modelName, description))

	return fmt.Sprintf("✓ Added '%s' as a tool!\n\nThe main model can now invoke it with: ask_%s\n\nDescription: %s",
		modelName, modelName, description), nil
}

// RemoveToolCommand allows disabling model tools
type RemoveToolCommand struct {
	cfg          *config.Config
	toolRegistry *tools.Registry
}

func NewRemoveToolCommand(cfg *config.Config, toolRegistry *tools.Registry) *RemoveToolCommand {
	return &RemoveToolCommand{cfg: cfg, toolRegistry: toolRegistry}
}

func (c *RemoveToolCommand) Name() string {
	return "removetool"
}

func (c *RemoveToolCommand) Description() string {
	return "Disable a model tool (usage: /removetool <model-name>)"
}

func (c *RemoveToolCommand) Execute(ctx context.Context, args []string, m *chatModel) (string, error) {
	if len(args) == 0 {
		var sb strings.Builder
		sb.WriteString("Enabled model tools:\n\n")

		found := false
		for _, mat := range c.cfg.ModelAsTools {
			if mat.Enabled {
				sb.WriteString(fmt.Sprintf("• %s - %s\n", mat.ModelName, mat.Description))
				found = true
			}
		}

		if !found {
			sb.WriteString("(none)\n")
		}

		sb.WriteString("\nUsage: /removetool <model-name>")
		return sb.String(), nil
	}

	modelName := args[0]

	// Find and disable
	found := false
	for i := range c.cfg.ModelAsTools {
		if c.cfg.ModelAsTools[i].ModelName == modelName && c.cfg.ModelAsTools[i].Enabled {
			c.cfg.ModelAsTools[i].Enabled = false
			found = true
			break
		}
	}

	if !found {
		return fmt.Sprintf("Model '%s' is not currently enabled as a tool", modelName), nil
	}

	if err := c.cfg.Save(); err != nil {
		return "", fmt.Errorf("failed to save config: %w", err)
	}

	return fmt.Sprintf("✓ Removed '%s' as a tool\n\nNote: Restart to fully unload the tool", modelName), nil
}

// AddAllToolsCommand enables all available models as tools
type AddAllToolsCommand struct {
	client       *ollama.Client
	cfg          *config.Config
	toolRegistry *tools.Registry
}

func NewAddAllToolsCommand(client *ollama.Client, cfg *config.Config, toolRegistry *tools.Registry) *AddAllToolsCommand {
	return &AddAllToolsCommand{client: client, cfg: cfg, toolRegistry: toolRegistry}
}

func (c *AddAllToolsCommand) Name() string {
	return "addalltools"
}

func (c *AddAllToolsCommand) Description() string {
	return "Enable all available models as tools"
}

func (c *AddAllToolsCommand) Execute(ctx context.Context, args []string, m *chatModel) (string, error) {
	models, err := c.client.ListModels(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to list models: %w", err)
	}

	currentModel := c.cfg.DefaultModel
	enabledTools := make(map[string]bool)
	for _, mat := range c.cfg.ModelAsTools {
		if mat.Enabled {
			enabledTools[mat.ModelName] = true
		}
	}

	addedCount := 0
	skippedCount := 0
	var addedModels []string

	for _, model := range models {
		// Skip current model
		if model.Name == currentModel {
			skippedCount++
			continue
		}

		// Skip already enabled
		if enabledTools[model.Name] {
			skippedCount++
			continue
		}

		// Auto-generate description from capabilities
		description := ""
		if cap, ok := c.cfg.ModelCapabilities[model.Name]; ok && len(cap.RecommendedFor) > 0 {
			description = fmt.Sprintf("Specialized in: %s", strings.Join(cap.RecommendedFor, ", "))
		} else {
			description = fmt.Sprintf("Ask the %s model for help", model.Name)
		}

		// Add to config
		c.cfg.ModelAsTools = append(c.cfg.ModelAsTools, config.ModelAsTool{
			ModelName:   model.Name,
			Description: description,
			Enabled:     true,
		})

		// Register the tool immediately
		c.toolRegistry.Register(tools.NewAskModelTool(c.client, model.Name, description))

		addedModels = append(addedModels, model.Name)
		addedCount++
	}

	if err := c.cfg.Save(); err != nil {
		return "", fmt.Errorf("failed to save config: %w", err)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("✓ Added %d models as tools\n", addedCount))
	if skippedCount > 0 {
		sb.WriteString(fmt.Sprintf("  Skipped %d (current model or already enabled)\n", skippedCount))
	}

	if len(addedModels) > 0 {
		sb.WriteString("\nAdded models:\n")
		for _, modelName := range addedModels {
			sb.WriteString(fmt.Sprintf("  • ask_%s\n", modelName))
		}
	}

	return sb.String(), nil
}
