# Llemecode Documentation

## Available Tools

Llemecode provides the following built-in tools that the AI can use:

### File Operations

- **read_file**: Read the contents of a file
  - Parameters: `path` (string)
  
- **write_file**: Write content to a file
  - Parameters: `path` (string), `content` (string)
  
- **list_files**: List files in a directory
  - Parameters: `path` (string), `recursive` (boolean, optional)

### Web Operations

- **web_fetch**: Fetch content from a URL
  - Parameters: `url` (string)

### System Operations

- **bash**: Execute a bash command
  - Parameters: `command` (string)

## Configuration

The configuration file is located at `~/.config/llemecode/config.json`.

### Structure

```json
{
  "ollama_url": "http://localhost:11434",
  "default_model": "llama3.2",
  "benchmark_tasks": [...],
  "system_prompts": {...},
  "model_capabilities": {...}
}
```

### Customizing System Prompts

You can edit the system prompts for different tool calling formats:

- `default`: For models with native tool support
- `tool_xml`: For XML-based tool calling
- `tool_json`: For JSON-based tool calling
- `tool_text`: For simple text-based tool calling

### Customizing Benchmark Tasks

Edit the `benchmark_tasks` array to add or modify evaluation tasks:

```json
{
  "name": "my_test",
  "description": "Description of what this tests",
  "prompt": "The prompt to send to the model",
  "category": "coding|reasoning|tool_use|creative|general"
}
```

### Model Capabilities

The system auto-detects model capabilities, but you can override them:

```json
{
  "model_capabilities": {
    "llama3.2": {
      "supports_tools": true,
      "tool_call_format": "native",
      "recommended_for": ["coding", "reasoning"]
    }
  }
}
```

Tool call formats:
- `native`: Ollama's built-in tool calling (preferred)
- `xml`: XML-tagged tool calls (for instruction-following models)
- `json`: JSON-formatted tool calls (for structured output models)
- `text`: Simple text patterns (for basic models)

## How Tool Calling Works

### Native Tool Calling

Models with native support (like Llama 3+) use Ollama's built-in tool calling API. The model decides when to call tools and the system executes them automatically.

### Fallback Formats

For models without native support, Llemecode teaches the model to use one of three formats:

#### XML Format
```xml
<tool_call>
<name>read_file</name>
<arguments>
{"path": "main.go"}
</arguments>
</tool_call>
```

#### JSON Format
```json
{
  "tool_call": {
    "name": "read_file",
    "arguments": {"path": "main.go"}
  }
}
```

#### Text Format
```
USE_TOOL: read_file
ARGS: {"path": "main.go"}
```

The system automatically parses these formats and executes the tools.

## Re-running Setup

To re-run the setup and re-benchmark your models:

```bash
rm ~/.config/llemecode/config.json
./llemecode
```

## Troubleshooting

### Ollama not available

Make sure Ollama is running:
```bash
ollama serve
```

### No models found

Pull at least one model:
```bash
ollama pull llama3.2
```

### Tool calling not working

Some models work better with different formats. You can manually set the format in `~/.config/llemecode/config.json` by changing the `tool_call_format` for your model from `native` to `xml`, `json`, or `text`.

## Project Structure

```
llemecode/
├── cmd/
│   └── llemecode/         # Main CLI entry point
├── internal/
│   ├── agent/             # Conversation agent with tool calling
│   ├── benchmark/         # Model detection and benchmarking
│   ├── cli/               # Bubbletea TUI components
│   ├── config/            # Configuration management
│   ├── ollama/            # Ollama API client
│   └── tools/             # Tool implementations
├── go.mod
└── README.md
```

## Contributing

Contributions are welcome! Areas for improvement:

- Additional tools (database queries, git operations, etc.)
- ACP server implementation for editor integration
- Project-specific `.llemecode.json` configuration
- Streaming responses in the TUI
- Better model evaluation metrics
- Tool result caching

## License

MIT License - see LICENSE file for details
