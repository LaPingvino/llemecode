# ACP (Anthropic Computer Protocol) Integration

Llemecode now supports running as an ACP server, allowing integration with editors like Zed, VSCode, and other tools that support the Anthropic Computer Protocol.

## What is ACP?

ACP (Anthropic Computer Protocol) is a JSON-RPC based protocol that allows editors and other applications to communicate with AI assistants. It provides a standard interface for:

- Tool calling (read files, write files, execute commands, etc.)
- Chat/conversation management
- Model selection and management

## Starting the ACP Server

To run Llemecode in ACP server mode:

```bash
llemecode --acp
```

The server will:
- Start listening on stdin/stdout for JSON-RPC messages
- Load your configuration from `~/.config/llemecode/config.json`
- Use your default model (or allow switching via protocol)
- Auto-approve all tool executions (the editor handles permission requests)

## Configuration

### First-time Setup

Before running in ACP mode, you should set up Llemecode at least once:

```bash
# Interactive model picker
llemecode

# Or full benchmark setup
llemecode --setup
```

This creates `~/.config/llemecode/config.json` with your model preferences and capabilities.

### Editor Configuration

Different editors have different ways to configure ACP servers. Here are some examples:

#### Zed Editor

Add to your Zed settings (`.config/zed/settings.json`):

```json
{
  "language_models": {
    "llemecode": {
      "adapter": "acp",
      "command": "/path/to/llemecode",
      "args": ["--acp"],
      "env": {}
    }
  }
}
```

#### Generic ACP Configuration

Most ACP-compatible editors will need:
- **Command**: Full path to the `llemecode` binary
- **Args**: `["--acp"]`
- **Protocol**: JSON-RPC over stdin/stdout

## Available Methods

The ACP server implements the following JSON-RPC methods:

### `initialize`

Initialize the connection and get server capabilities.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "initialize"
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "protocolVersion": "0.1.0",
    "serverInfo": {
      "name": "llemecode",
      "version": "0.1.0"
    },
    "capabilities": {
      "tools": true,
      "chat": true
    }
  }
}
```

### `tools/list`

List all available tools.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "tools/list"
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "result": {
    "tools": [
      {
        "name": "read_file",
        "description": "Read contents of a file",
        "inputSchema": {
          "type": "object",
          "properties": {
            "path": {
              "type": "string",
              "description": "Path to the file to read"
            }
          },
          "required": ["path"]
        }
      }
      // ... more tools
    ]
  }
}
```

### `tools/call`

Execute a tool.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "method": "tools/call",
  "params": {
    "name": "read_file",
    "arguments": {
      "path": "./README.md"
    }
  }
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "result": {
    "content": [
      {
        "type": "text",
        "text": "# Llemecode\n\n..."
      }
    ]
  }
}
```

### `chat`

Send a chat message to the LLM.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "id": 4,
  "method": "chat",
  "params": {
    "message": "Read the main.go file and explain what it does",
    "model": "llama3.2"  // Optional: override default model
  }
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 4,
  "result": {
    "content": [
      {
        "type": "tool_use",
        "name": "read_file",
        "input": {"path": "main.go"}
      },
      {
        "type": "tool_result",
        "text": "package main\n..."
      },
      {
        "type": "text",
        "text": "This is the main entry point for Llemecode..."
      }
    ]
  }
}
```

### `models/list`

List available Ollama models.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "id": 5,
  "method": "models/list"
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 5,
  "result": {
    "models": [
      {
        "name": "llama3.2",
        "size": 2042365184,
        "supports_tools": true,
        "tool_format": "native",
        "recommended_for": ["coding", "reasoning"]
      }
    ],
    "default_model": "llama3.2"
  }
}
```

### `models/switch`

Switch the active model.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "id": 6,
  "method": "models/switch",
  "params": {
    "model": "qwen2.5-coder"
  }
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 6,
  "result": {
    "model": "qwen2.5-coder"
  }
}
```

## Available Tools

When running in ACP mode, Llemecode provides these tools:

### File Operations

- **read_file**: Read file contents
- **write_file**: Write or modify files
- **list_files**: List directory contents (with optional recursion)

### System Operations

- **bash**: Execute shell commands
- **web_fetch**: Fetch content from URLs

### Model Operations

- **ask_model_***: If you have model-as-tool configured in your config, specialized models will be available as tools

## Permissions

In ACP mode, **all permission checks are auto-approved**. This is intentional because:

1. The editor/user is already initiating the action
2. The editor typically has its own permission system
3. It provides a smoother user experience

If you need stricter permissions, you can:
- Use the interactive chat mode instead (`llemecode` without `--acp`)
- Configure `blocked_commands` in your config to prevent dangerous bash commands
- Modify tool permissions in your `~/.config/llemecode/config.json`

## Debugging

### Check if ACP server is working

You can test the ACP server manually:

```bash
echo '{"jsonrpc":"2.0","id":1,"method":"initialize"}' | llemecode --acp
```

Expected output (to stderr and stdout):
```
Llemecode ACP server started
{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"0.1.0","serverInfo":{"name":"llemecode","version":"0.1.0"},"capabilities":{"tools":true,"chat":true}}}
```

### Common Issues

**"No default model configured"**
- Run `llemecode` or `llemecode --setup` first to select a model

**"Ollama is not available"**
- Make sure Ollama is running: `ollama serve`
- Check Ollama URL in config: `~/.config/llemecode/config.json`

**Tools not executing**
- Check that your Ollama models are pulled: `ollama list`
- Verify model capabilities in config

**Editor not connecting**
- Check the full path to `llemecode` binary
- Ensure `--acp` flag is passed
- Look at editor logs for connection errors

## Advanced Configuration

### Disabling Tools

You can disable specific tools in your config:

```json
{
  "disabled_tools": ["bash", "web_fetch"]
}
```

This prevents the LLM from using these tools in both ACP and chat modes.

### Custom Model Capabilities

Override auto-detected model capabilities:

```json
{
  "model_capabilities": {
    "my-custom-model": {
      "supports_tools": true,
      "tool_call_format": "xml",
      "recommended_for": ["coding"]
    }
  }
}
```

### Model-as-Tool

Configure specialized models as tools:

```json
{
  "model_as_tools": [
    {
      "model_name": "qwen2.5-coder",
      "description": "Expert coding model for complex programming tasks",
      "enabled": true
    }
  ]
}
```

## Performance Tips

1. **Model Selection**: Smaller models (like `llama3.2`) respond faster for simple tasks
2. **Tool Format**: Native tool calling is fastest when supported
3. **Benchmarking**: Run benchmarks to find the best model for your use case:
   ```bash
   llemecode --benchmark --evaluator your-best-model
   ```

## Examples

### Using with Zed

1. Install and configure Llemecode:
   ```bash
   llemecode --setup
   ```

2. Add to Zed settings:
   ```json
   {
     "language_models": {
       "llemecode": {
         "adapter": "acp",
         "command": "/usr/local/bin/llemecode",
         "args": ["--acp"]
       }
     }
   }
   ```

3. Use in Zed:
   - Open a project
   - Ask the assistant to read/modify files
   - The LLM will use Llemecode's tools automatically

### Using Programmatically

```python
import json
import subprocess

# Start ACP server
proc = subprocess.Popen(
    ["llemecode", "--acp"],
    stdin=subprocess.PIPE,
    stdout=subprocess.PIPE,
    stderr=subprocess.PIPE,
    text=True
)

# Send initialize request
request = {"jsonrpc": "2.0", "id": 1, "method": "initialize"}
proc.stdin.write(json.dumps(request) + "\n")
proc.stdin.flush()

# Read response
response = json.loads(proc.stdout.readline())
print(response)
```

## Troubleshooting

For more help:
- Check main README: `/path/to/llemecode/README.md`
- Run interactive mode to test: `llemecode`
- List available models: `llemecode --list`
- Re-benchmark models: `llemecode --benchmark`

## Future Enhancements

Planned features for ACP mode:
- Streaming responses
- Conversation history management
- Tool approval callbacks for editor integration
- Workspace-specific configurations
