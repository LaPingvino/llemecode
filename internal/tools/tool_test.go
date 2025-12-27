package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestReadFileTool(t *testing.T) {
	tool := NewReadFileTool()

	// Test metadata
	if tool.Name() != "read_file" {
		t.Errorf("Expected name 'read_file', got '%s'", tool.Name())
	}

	// Create temp file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	content := "Hello, World!"
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Test reading
	ctx := context.Background()
	args := map[string]interface{}{
		"path": testFile,
	}

	result, err := tool.Execute(ctx, args)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result != content {
		t.Errorf("Expected content '%s', got '%s'", content, result)
	}

	// Test missing file
	args["path"] = filepath.Join(tmpDir, "nonexistent.txt")
	_, err = tool.Execute(ctx, args)
	if err == nil {
		t.Error("Expected error for nonexistent file")
	}
}

func TestWriteFileTool(t *testing.T) {
	tool := NewWriteFileTool()

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "subdir", "test.txt")
	content := "Test content"

	ctx := context.Background()
	args := map[string]interface{}{
		"path":    testFile,
		"content": content,
	}

	result, err := tool.Execute(ctx, args)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result == "" {
		t.Error("Expected non-empty result")
	}

	// Verify file was created
	data, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read created file: %v", err)
	}

	if string(data) != content {
		t.Errorf("Expected content '%s', got '%s'", content, string(data))
	}
}

func TestListFilesTool(t *testing.T) {
	tool := NewListFilesTool()

	tmpDir := t.TempDir()

	// Create test files
	os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("test"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "file2.txt"), []byte("test"), 0644)
	os.Mkdir(filepath.Join(tmpDir, "subdir"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "subdir", "file3.txt"), []byte("test"), 0644)

	ctx := context.Background()

	// Test non-recursive
	args := map[string]interface{}{
		"path":      tmpDir,
		"recursive": false,
	}

	result, err := tool.Execute(ctx, args)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result == "" {
		t.Error("Expected non-empty result")
	}

	// Test recursive
	args["recursive"] = true
	result, err = tool.Execute(ctx, args)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result == "" {
		t.Error("Expected non-empty result")
	}
}

func TestToolRegistry(t *testing.T) {
	registry := NewRegistry()

	readTool := NewReadFileTool()
	writeTool := NewWriteFileTool()

	registry.Register(readTool)
	registry.Register(writeTool)

	// Test Get
	tool, ok := registry.Get("read_file")
	if !ok {
		t.Error("Expected to find read_file tool")
	}
	if tool.Name() != "read_file" {
		t.Errorf("Expected tool name 'read_file', got '%s'", tool.Name())
	}

	// Test missing tool
	_, ok = registry.Get("nonexistent")
	if ok {
		t.Error("Expected not to find nonexistent tool")
	}

	// Test All
	tools := registry.All()
	if len(tools) != 2 {
		t.Errorf("Expected 2 tools, got %d", len(tools))
	}

	// Test Execute
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	os.WriteFile(testFile, []byte("content"), 0644)

	args := map[string]interface{}{
		"path": testFile,
	}

	ctx := context.Background()
	result, err := registry.Execute(ctx, "read_file", args)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result != "content" {
		t.Errorf("Expected 'content', got '%s'", result)
	}

	// Test execute nonexistent tool
	_, err = registry.Execute(ctx, "nonexistent", args)
	if err == nil {
		t.Error("Expected error for nonexistent tool")
	}
}
