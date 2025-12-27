package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/LaPingvino/llemecode/internal/acp"
	"github.com/LaPingvino/llemecode/internal/benchmark"
	"github.com/LaPingvino/llemecode/internal/cli"
	"github.com/LaPingvino/llemecode/internal/config"
	"github.com/LaPingvino/llemecode/internal/logger"
	"github.com/LaPingvino/llemecode/internal/mcp"
	"github.com/LaPingvino/llemecode/internal/ollama"
	"github.com/LaPingvino/llemecode/internal/tools"
	"github.com/spf13/pflag"
)

var (
	modelFlag      = pflag.StringP("model", "m", "", "Override the default model")
	benchmarkFlag  = pflag.BoolP("benchmark", "b", false, "Run benchmarks and update configuration")
	listModelsFlag = pflag.BoolP("list", "l", false, "List available models and their capabilities")
	setupFlag      = pflag.BoolP("setup", "s", false, "Force re-run first-time setup")
	evaluatorModel = pflag.String("evaluator", "", "Model to use for evaluating benchmark results")
	acpFlag        = pflag.Bool("acp", false, "Run in ACP (Anthropic Computer Protocol) server mode")
	helpFlag       = pflag.BoolP("help", "h", false, "Show help message")
	logToFile      = pflag.String("log-to-file", "", "Log debug output and conversation to file")
)

func main() {
	pflag.Parse()

	if *helpFlag {
		printHelp()
		os.Exit(0)
	}

	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func printHelp() {
	fmt.Println("Llemecode - Local LLM coding assistant with Ollama")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  llemecode [flags]")
	fmt.Println()
	fmt.Println("Flags:")
	pflag.PrintDefaults()
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  llemecode                          # Start chat with default model")
	fmt.Println("  llemecode -m llama3.2              # Use specific model")
	fmt.Println("  llemecode -b                       # Re-run benchmarks")
	fmt.Println("  llemecode -s                       # Re-run first-time setup")
	fmt.Println("  llemecode -l                       # List available models")
	fmt.Println("  llemecode -b --evaluator gpt-oss   # Benchmark with AI evaluation")
}

func run() error {
	// Initialize logger if requested
	if *logToFile != "" {
		if err := logger.Init(*logToFile); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Failed to initialize logging: %v\n", err)
		} else {
			defer logger.Close()
			logger.Log("Llemecode starting with logging enabled")
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		logger.Log("Received interrupt signal")
		cancel()
	}()

	// Load or create config
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Create Ollama client
	client := ollama.NewClient(cfg.OllamaURL)

	// Check if Ollama is available
	if !client.IsAvailable(ctx) {
		return fmt.Errorf("Ollama is not available at %s. Please ensure Ollama is running", cfg.OllamaURL)
	}

	// Handle list models flag
	if *listModelsFlag {
		return listModels(ctx, client, cfg)
	}

	// Handle setup/benchmark flags
	// Only trigger setup if there's NO default model
	// Model capabilities can be populated later by background benchmarking
	needsSetup := cfg.DefaultModel == ""

	if *setupFlag || *benchmarkFlag {
		// Explicit setup/benchmark request - use traditional flow
		if needsSetup {
			fmt.Println("🚀 Welcome to Llemecode!")
			fmt.Println("Running first-time setup to detect and benchmark your models...")
		} else if *benchmarkFlag {
			fmt.Println("🔄 Re-running benchmarks...")
		} else {
			fmt.Println("🔧 Running setup...")
		}
		fmt.Println()

		// If evaluator model specified, use it
		if *evaluatorModel != "" {
			cfg.DefaultModel = *evaluatorModel
			if err := cfg.Save(); err != nil {
				return fmt.Errorf("save config: %w", err)
			}
		}

		if err := cli.RunSetup(ctx, client, cfg); err != nil {
			return fmt.Errorf("setup failed: %w", err)
		}

		// Reload config after setup
		cfg, err = config.Load()
		if err != nil {
			return fmt.Errorf("reload config: %w", err)
		}

		// If this was just a benchmark run, exit
		if *benchmarkFlag && !needsSetup {
			fmt.Println("\n✓ Benchmarks complete!")
			fmt.Printf("Results saved to: %s\n", mustGetConfigDir()+"/benchmark_results.json")
			return nil
		}
	} else if needsSetup {
		// First run - use interactive model picker
		selectedModel, err := cli.RunModelPicker(ctx, client)
		if err != nil {
			return fmt.Errorf("model selection failed: %w", err)
		}

		cfg.DefaultModel = selectedModel

		// Immediately test the selected model's tool capabilities
		fmt.Printf("\n✓ Selected %s as your default model\n", selectedModel)
		fmt.Println("🔍 Testing tool capabilities...")

		benchmarker := benchmark.New(client, cfg.BenchmarkTasks)
		if err := benchmarker.DetectToolSupport(ctx, selectedModel, cfg); err != nil {
			fmt.Printf("⚠️  Warning: Could not detect tool support: %v\n", err)
		} else {
			fmt.Printf("✓ Tool support detected and configured\n")
		}

		// Save config with tool capabilities
		if err := cfg.Save(); err != nil {
			return fmt.Errorf("save config: %w", err)
		}

		fmt.Println("📊 Full benchmarking will run in the background to evaluate all models...")
		fmt.Println()
	}

	// Override model if specified
	if *modelFlag != "" {
		cfg.DefaultModel = *modelFlag
		fmt.Printf("Using model: %s\n", cfg.DefaultModel)
	}

	// Validate we have a model
	if cfg.DefaultModel == "" {
		return fmt.Errorf("no default model configured. Run with --setup or specify --model")
	}

	// Create tool registry and register tools
	toolRegistry, memTracker, messageChannel := setupTools(ctx, client, cfg, *acpFlag)
	_ = memTracker     // TODO: Use for tracking
	_ = messageChannel // TODO: Use for model communication

	// Start background benchmarking if first run
	var bgBenchmark *cli.BackgroundBenchmark
	if needsSetup && !*setupFlag && !*benchmarkFlag {
		benchmarker := benchmark.New(client, cfg.BenchmarkTasks)
		if *evaluatorModel != "" {
			benchmarker.SetEvaluator(*evaluatorModel)
		} else if cfg.DefaultModel != "" {
			benchmarker.SetEvaluator(cfg.DefaultModel)
		}
		bgBenchmark = cli.NewBackgroundBenchmark(ctx, benchmarker, cfg)
		bgBenchmark.Start()
	}

	// Run in ACP mode or chat mode
	if *acpFlag {
		return runACPMode(ctx, client, cfg, toolRegistry)
	}

	// Run chat interface
	return cli.RunChat(ctx, client, cfg, toolRegistry, bgBenchmark)
}

func setupTools(ctx context.Context, client *ollama.Client, cfg *config.Config, acpMode bool) (*tools.Registry, *tools.ModelMemoryTracker, *tools.MessageChannel) {
	toolRegistry := tools.NewRegistry()

	// Create shared infrastructure
	memTracker := tools.NewModelMemoryTracker()
	messageChannel := tools.NewMessageChannel()
	mcpRegistry := mcp.NewMCPToolRegistry()

	// Load MCP servers from config
	for _, mcpServer := range cfg.MCPServers {
		if !mcpServer.Enabled {
			continue
		}

		if err := mcpRegistry.AddServer(ctx, mcpServer.Name, mcpServer.Command, mcpServer.Args); err != nil {
			if !acpMode {
				fmt.Fprintf(os.Stderr, "⚠️ Failed to start MCP server %s: %v\n", mcpServer.Name, err)
			}
			continue
		}

		if !acpMode {
			fmt.Printf("✓ Connected to MCP server: %s\n", mcpServer.Name)
		}
	}

	// Create permission checker - different for ACP vs chat mode
	var permChecker tools.PermissionChecker
	if acpMode {
		// In ACP mode, auto-approve everything (editor handles permissions)
		permChecker = tools.NewAutoApproveChecker()
	} else {
		// In chat mode, use interactive permission checker
		permChecker = cli.NewChatPermissionChecker()
	}

	// Convert config permissions to tool permissions
	toolPermConfig := &tools.PermissionConfig{
		AutoApproveSafe:        cfg.Permissions.AutoApproveSafe,
		AutoApproveRead:        cfg.Permissions.AutoApproveRead,
		RequireApprovalWrite:   cfg.Permissions.RequireApprovalWrite,
		RequireApprovalExecute: cfg.Permissions.RequireApprovalExecute,
		RequireApprovalNetwork: cfg.Permissions.RequireApprovalNetwork,
		BlockedCommands:        cfg.Permissions.BlockedCommands,
	}

	// Register built-in tools with permission levels
	toolRegistry.Register(tools.NewProtectedTool(
		tools.NewReadFileTool(), tools.PermissionRead, permChecker, toolPermConfig))
	toolRegistry.Register(tools.NewProtectedTool(
		tools.NewWriteFileTool(), tools.PermissionWrite, permChecker, toolPermConfig))
	toolRegistry.Register(tools.NewProtectedTool(
		tools.NewListFilesTool(), tools.PermissionRead, permChecker, toolPermConfig))
	toolRegistry.Register(tools.NewProtectedTool(
		tools.NewReadBenchmarkTool(), tools.PermissionRead, permChecker, toolPermConfig))
	toolRegistry.Register(tools.NewProtectedTool(
		tools.NewWebFetchTool(), tools.PermissionNetwork, permChecker, toolPermConfig))

	// Create bash tool with interactive executor (only in chat mode, not ACP)
	bashTool := tools.NewBashTool()
	if !acpMode {
		bashTool.SetExecutor(cli.NewInteractiveCommandExecutor())
	} else {
		// In ACP mode, use simple executor without interactive window
		bashTool.SetExecutor(cli.NewSimpleCommandExecutor())
	}
	toolRegistry.Register(tools.NewProtectedTool(
		bashTool, tools.PermissionExecute, permChecker, toolPermConfig))

	// Register model-as-tool (if configured)
	for _, mat := range cfg.ModelAsTools {
		if mat.Enabled {
			toolRegistry.Register(tools.NewProtectedTool(
				tools.NewAskModelTool(client, mat.ModelName, mat.Description),
				tools.PermissionSafe, permChecker, toolPermConfig))
			if !acpMode {
				fmt.Printf("✓ Registered model as tool: %s\n", mat.ModelName)
			}
		}
	}

	// Register memory management tools
	toolRegistry.Register(tools.NewProtectedTool(
		tools.NewMemoryStatusTool(), tools.PermissionSafe, permChecker, toolPermConfig))
	toolRegistry.Register(tools.NewProtectedTool(
		tools.NewModelMemoryReportTool(memTracker), tools.PermissionSafe, permChecker, toolPermConfig))
	toolRegistry.Register(tools.NewProtectedTool(
		tools.NewGarbageCollectModelsTool(memTracker), tools.PermissionSafe, permChecker, toolPermConfig))

	// Register communication tools
	toolRegistry.Register(tools.NewProtectedTool(
		tools.NewReceiveMessagesTool(messageChannel), tools.PermissionSafe, permChecker, toolPermConfig))

	// Register tool management tools
	toolRegistry.Register(tools.NewProtectedTool(
		tools.NewAddCustomToolTool(toolRegistry, cfg), tools.PermissionWrite, permChecker, toolPermConfig))
	toolRegistry.Register(tools.NewProtectedTool(
		tools.NewRemoveCustomToolTool(toolRegistry, cfg), tools.PermissionWrite, permChecker, toolPermConfig))
	toolRegistry.Register(tools.NewProtectedTool(
		tools.NewListCustomToolsTool(cfg), tools.PermissionSafe, permChecker, toolPermConfig))

	// Register MCP management tools
	toolRegistry.Register(tools.NewProtectedTool(
		mcp.NewAddMCPServerTool(mcpRegistry, cfg, toolRegistry, ctx), tools.PermissionExecute, permChecker, toolPermConfig))
	toolRegistry.Register(tools.NewProtectedTool(
		mcp.NewRemoveMCPServerTool(cfg), tools.PermissionWrite, permChecker, toolPermConfig))
	toolRegistry.Register(tools.NewProtectedTool(
		mcp.NewListMCPServersTool(cfg, mcpRegistry), tools.PermissionSafe, permChecker, toolPermConfig))

	// Load custom tools from config
	for _, customToolData := range cfg.CustomTools {
		customTool, err := tools.DeserializeCustomTool(customToolData)
		if err != nil {
			if !acpMode {
				fmt.Fprintf(os.Stderr, "⚠️ Failed to load custom tool: %v\n", err)
			}
			continue
		}
		toolRegistry.Register(tools.NewProtectedTool(
			customTool, tools.PermissionExecute, permChecker, toolPermConfig))
		if !acpMode {
			fmt.Printf("✓ Loaded custom tool: %s\n", customTool.Name())
		}
	}

	// Register MCP tools
	mcpTools := mcpRegistry.GetTools()
	for _, mcpTool := range mcpTools {
		// MCP tools get Network permission level (they communicate with external processes)
		toolRegistry.Register(tools.NewProtectedTool(
			mcpTool, tools.PermissionNetwork, permChecker, toolPermConfig))
		if !acpMode {
			fmt.Printf("✓ Loaded MCP tool: %s\n", mcpTool.Name())
		}
	}

	return toolRegistry, memTracker, messageChannel
}

func runACPMode(ctx context.Context, client *ollama.Client, cfg *config.Config, toolRegistry *tools.Registry) error {
	server := acp.NewServer(client, cfg, toolRegistry)
	fmt.Fprintf(os.Stderr, "Llemecode ACP server started\n")
	return server.Start(ctx)
}

func listModels(ctx context.Context, client *ollama.Client, cfg *config.Config) error {
	models, err := client.ListModels(ctx)
	if err != nil {
		return fmt.Errorf("list models: %w", err)
	}

	fmt.Println("Available Models:")
	fmt.Println()

	for _, model := range models {
		fmt.Printf("📦 %s\n", model.Name)

		if cap, ok := cfg.ModelCapabilities[model.Name]; ok {
			fmt.Printf("   Tool Support: %v\n", cap.SupportsTools)
			fmt.Printf("   Tool Format: %s\n", cap.ToolCallFormat)
			if len(cap.RecommendedFor) > 0 {
				fmt.Printf("   Best For: %v\n", cap.RecommendedFor)
			}
		} else {
			fmt.Printf("   (Not yet benchmarked - run with --benchmark to evaluate)\n")
		}

		if model.Name == cfg.DefaultModel {
			fmt.Printf("   ⭐ DEFAULT MODEL\n")
		}

		fmt.Println()
	}

	return nil
}

func mustGetConfigDir() string {
	dir, _ := config.GetConfigDir()
	return dir
}
