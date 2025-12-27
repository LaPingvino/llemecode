package tools

import (
	"context"
	"encoding/json"
)

type Tool interface {
	Name() string
	Description() string
	Parameters() map[string]interface{}
	Execute(ctx context.Context, args map[string]interface{}) (string, error)
}

type Registry struct {
	tools map[string]Tool
}

func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]Tool),
	}
}

func (r *Registry) Register(tool Tool) {
	r.tools[tool.Name()] = tool
}

func (r *Registry) Unregister(name string) {
	delete(r.tools, name)
}

func (r *Registry) Get(name string) (Tool, bool) {
	tool, ok := r.tools[name]
	return tool, ok
}

func (r *Registry) All() []Tool {
	tools := make([]Tool, 0, len(r.tools))
	for _, tool := range r.tools {
		tools = append(tools, tool)
	}
	return tools
}

// AllFiltered returns all tools excluding those in the disabled list
func (r *Registry) AllFiltered(disabledTools []string) []Tool {
	disabledMap := make(map[string]bool)
	for _, name := range disabledTools {
		disabledMap[name] = true
	}

	tools := make([]Tool, 0, len(r.tools))
	for name, tool := range r.tools {
		if !disabledMap[name] {
			tools = append(tools, tool)
		}
	}
	return tools
}

func (r *Registry) Execute(ctx context.Context, name string, args map[string]interface{}) (string, error) {
	tool, ok := r.Get(name)
	if !ok {
		return "", ErrToolNotFound{Name: name}
	}
	return tool.Execute(ctx, args)
}

type ErrToolNotFound struct {
	Name string
}

func (e ErrToolNotFound) Error() string {
	return "tool not found: " + e.Name
}

func ToOllamaTools(tools []Tool) []map[string]interface{} {
	ollamaTools := make([]map[string]interface{}, len(tools))
	for i, tool := range tools {
		ollamaTools[i] = map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name":        tool.Name(),
				"description": tool.Description(),
				"parameters":  tool.Parameters(),
			},
		}
	}
	return ollamaTools
}

func ParseArgs(argsJSON string, target interface{}) error {
	return json.Unmarshal([]byte(argsJSON), target)
}
