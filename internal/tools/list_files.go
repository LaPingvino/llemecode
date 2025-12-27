package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type ListFilesTool struct{}

func NewListFilesTool() *ListFilesTool {
	return &ListFilesTool{}
}

func (t *ListFilesTool) Name() string {
	return "list_files"
}

func (t *ListFilesTool) Description() string {
	return "List files in a directory, optionally recursively"
}

func (t *ListFilesTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Path to the directory to list",
			},
			"recursive": map[string]interface{}{
				"type":        "boolean",
				"description": "Whether to list files recursively",
			},
		},
		"required": []string{"path"},
	}
}

func (t *ListFilesTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	path, ok := args["path"].(string)
	if !ok {
		return "", fmt.Errorf("path must be a string")
	}

	recursive := false
	if r, ok := args["recursive"].(bool); ok {
		recursive = r
	}

	var files []string
	if recursive {
		err := filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			files = append(files, p)
			return nil
		})
		if err != nil {
			return "", fmt.Errorf("walk directory: %w", err)
		}
	} else {
		entries, err := os.ReadDir(path)
		if err != nil {
			return "", fmt.Errorf("read directory: %w", err)
		}
		for _, entry := range entries {
			files = append(files, filepath.Join(path, entry.Name()))
		}
	}

	return strings.Join(files, "\n"), nil
}
