# Advanced Features

Llemecode includes several advanced features that enable the LLM to manage its own toolkit, monitor memory usage, and create specialized tools on-the-fly.

## Dynamic Tool Management

The LLM can create, list, and remove custom command-line tools during a conversation. This allows it to build specialized utilities for specific tasks without requiring code changes.

### Available Tools

#### `add_custom_tool`

Create a new custom command-line tool that wraps a shell command with named parameters.

**Parameters:**
- `name` (string, required): Name of the tool (alphanumeric and underscores only)
- `description` (string, required): Description of what the tool does  
- `command` (string, required): Shell command template with `{{param_name}}` placeholders
- `params` (array, optional): List of parameters the tool accepts
  - `name` (string): Parameter name
  - `type` (string): Parameter type (string, number, boolean)
  - `description` (string): Parameter description
  - `required` (boolean): Whether the parameter is required

**Example:**

The LLM can create a specialized Git status checker:

```json
{
  "name": "check_git_status",
  "description": "Check git status in a specific directory",
  "command": "cd {{directory}} && git status --short",
  "params": [
    {
      "name": "directory",
      "type": "string",
      "description": "Directory to check",
      "required": true
    }
  ]
}
```

After creation, the LLM can call `check_git_status` with `{"directory": "/path/to/repo"}`.

#### `remove_custom_tool`

Remove a previously created custom tool.

**Parameters:**
- `name` (string, required): Name of the custom tool to remove

#### `list_custom_tools`

List all custom tools that have been created.

**Parameters:** None

**Returns:** JSON list of all custom tools with their configurations.

### How It Works

1. **Creation**: When the LLM calls `add_custom_tool`, the tool is:
   - Registered in the tool registry (immediately available)
   - Saved to `~/.config/llemecode/config.json` (persists across sessions)

2. **Persistence**: Custom tools are automatically loaded on startup from the config file

3. **Security**: Custom tools run with `PermissionExecute` level, requiring user approval based on your permission settings

### Use Cases

- **Project-specific tools**: Create tools for linting, building, or testing specific to your project
- **Workflow automation**: Build tools that chain multiple commands together
- **Data processing**: Create tools for parsing or transforming data in specific formats
- **API interactions**: Wrap curl commands with cleaner parameter interfaces

### Example Session

```
User: Create a tool to count lines of code in a directory