# Llemecode (Let me code with Ollama)

Llemecode is a Go implementation of a command line LLM coding tool that uses your local Ollama instance. It features an intelligent model selection system, multi-format tool calling, and a beautiful terminal UI built with Bubbletea.

## Features

- 🤖 **Smart Model Detection**: Automatically discovers and benchmarks your local Ollama models on first run
- 🛠️ **Universal Tool Support**: Works with models that support native tool calling AND those that don't (via XML/JSON/text fallback formats)
- 💬 **Beautiful TUI**: Interactive chat interface with syntax highlighting and markdown rendering
- 🎯 **Model Capabilities**: Automatically detects which tool format works best for each model
- 🧠 **AI-Assisted Evaluation**: Use a powerful model to evaluate other models' benchmark results
- ⚙️ **Configurable**: All prompts and benchmarks customizable via `~/.config/llemecode/config.json`
- 🔄 **Re-run Benchmarks**: Easily re-evaluate models anytime with `--benchmark`
- 📁 **Built-in Tools**: File operations, web fetch, bash execution, and more

## Installation

### Prerequisites

- Go 1.21 or later
- [Ollama](https://ollama.ai/) installed and running
- At least one model pulled in Ollama (e.g., `ollama pull llama3.2`)

### Build from source

```bash
git clone https://github.com/LaPingvino/llemecode.git
cd llemecode
go build -o llemecode ./cmd/llemecode
```

### Install

```bash
# Install to /usr/local/bin
sudo cp llemecode /usr/local/bin/

# OR add to your PATH
export PATH=$PATH:$(pwd)
```

## Quick Start

### First Run

When you run Llemecode for the first time, it will:

1. Discover all models in your local Ollama instance
2. Test each model for native tool calling support
3. Determine the best fallback format (XML/JSON/text) for models without native support
4. Run benchmark tasks to evaluate model strengths
5. Select the best model as your default
6. Save configuration to `~/.config/llemecode/config.json`

```bash
./llemecode
```

You'll see a beautiful progress interface showing the detection and benchmarking process!

### Using a Powerful Model for Evaluation

For more accurate benchmarks, specify an evaluator model to assess other models:

```bash
# Use gpt-oss (or another strong model) to evaluate benchmarks
./llemecode --setup --evaluator gpt-oss
```

This will:
- Use gpt-oss to score each model's responses
- Generate detailed descriptions of each model's capabilities
- Provide reasoning for scores

## Usage

### Basic Chat

```bash
# Start chat with default model
./llemecode

# Use a specific model
./llemecode -m llama3.2

# Use a specific model (long form)
./llemecode --model qwen2.5-coder
```

### Listing Models

```bash
# See all available models and their capabilities
./llemecode -l
./llemecode --list
```

Example output:
```
📦 llama3.2
   Tool Support: true
   Tool Format: native
   Best For: [coding reasoning]
   ⭐ DEFAULT MODEL

📦 phi3
   Tool Support: false
   Tool Format: json
   Best For: [coding]
```

### Re-running Benchmarks

```bash
# Re-benchmark all models with simple heuristics
./llemecode -b

# Re-benchmark with AI evaluation
./llemecode --benchmark --evaluator gpt-oss
```

### Force Re-setup

```bash
# Completely re-run first-time setup
./llemecode -s
./llemecode --setup
```

### Help

```bash
./llemecode -h
./llemecode --help
```

## Chat Interface

Once in the chat:

- Type your message and press **Enter** to send
- The AI can use tools automatically (read files, run commands, fetch web content)
- Responses are rendered with beautiful markdown formatting
- Tool calls are displayed with their arguments and results
- Press **Esc** or **Ctrl+C** to quit

### Example Interactions

```
You: Read the contents of main.go and explain what it does

🔧 Tool: read_file
Arguments:
  "path": "main.go"
✅ Result:
[file contents...]
Assistant: This is the main entry point...

You: What files are in the current directory?

🔧 Tool: list_files
Arguments:
  "path": "."
  "recursive": false
✅ Result:
main.go
go.mod
README.md
...
```

## Available Tools

- **read_file**: Read file contents
- **write_file**: Write to a file
- **list_files**: List directory contents (with optional recursive flag)
- **web_fetch**: Fetch content from a URL
- **bash**: Execute bash commands

## Configuration

Configuration is stored at `~/.config/llemecode/config.json`.

### Customizing System Prompts

Edit the `system_prompts` section to change how the AI behaves for different tool formats:

```json
{
  "system_prompts": {
    "default": "Your custom prompt for native tool calling...",
    "tool_xml": "Custom prompt for XML format...",
    "tool_json": "Custom prompt for JSON format...",
    "tool_text": "Custom prompt for text format..."
  }
}
```

### Customizing Benchmark Tasks

Add or modify tasks in the `benchmark_tasks` array:

```json
{
  "benchmark_tasks": [
    {
      "name": "my_custom_test",
      "description": "Tests something specific",
      "prompt": "Your test prompt here",
      "category": "coding"
    }
  ]
}
```

Categories: `coding`, `reasoning`, `tool_use`, `creative`, `general`

### Manually Configuring Model Capabilities

Override auto-detected capabilities:

```json
{
  "model_capabilities": {
    "my-model": {
      "supports_tools": false,
      "tool_call_format": "xml",
      "recommended_for": ["coding"]
    }
  }
}
```

Tool formats: `native`, `xml`, `json`, `text`

## How It Works

### Tool Calling Strategies

**Native (Preferred)**: Models like Llama 3+ use Ollama's built-in tool API

**XML Fallback**: For instruction-following models
```xml
<tool_call>
<name>read_file</name>
<arguments>{"path": "file.txt"}</arguments>
</tool_call>
```

**JSON Fallback**: For structured output models
```json
{
  "tool_call": {
    "name": "read_file",
    "arguments": {"path": "file.txt"}
  }
}
```

**Text Fallback**: For basic models
```
USE_TOOL: read_file
ARGS: {"path": "file.txt"}
```

### AI-Assisted Evaluation

When using `--evaluator`, a powerful model:
1. Evaluates each benchmark response for quality
2. Provides scores from 0.0 to 1.0
3. Gives reasoning for scores
4. Generates descriptive summaries

This gives much more accurate results than simple heuristics!

## Troubleshooting

**Ollama not available**
```bash
# Start Ollama
ollama serve
```

**No models found**
```bash
# Pull a model
ollama pull llama3.2
```

**Tool calling not working**

Try a different format in config.json. Change `tool_call_format` from `native` to `xml`, `json`, or `text`.

**Poor benchmark results**

Re-run with AI evaluation:
```bash
./llemecode --benchmark --evaluator your-best-model
```

## Project Structure

```
llemecode/
├── cmd/llemecode/          # CLI entry point
├── internal/
│   ├── agent/              # Conversation & tool execution
│   ├── benchmark/          # Model detection & evaluation
│   ├── cli/                # Bubbletea UI components
│   ├── config/             # Configuration management
│   ├── ollama/             # Ollama API client
│   └── tools/              # Tool implementations
├── README.md
├── DOCS.md                 # Additional documentation
└── go.mod
```

## Future Enhancements

- ACP server for Zed/editor integration
- Project-specific `.llemecode.json` configuration
- Streaming responses in TUI
- Additional tools (git, database, etc.)
- Model-as-tool (use specialized models for specific tasks)

## Contributing

Contributions welcome! Please open issues or PRs.

## License

MIT License

## Credits

Built with:
- [Ollama](https://ollama.ai/) - Local LLM runtime
- [Bubbletea](https://github.com/charmbracelet/bubbletea) - Terminal UI framework
- [Glamour](https://github.com/charmbracelet/glamour) - Markdown rendering
- [Lipgloss](https://github.com/charmbracelet/lipgloss) - Terminal styling
