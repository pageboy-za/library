package mcpserver

import (
	"context"
	"encoding/json"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"home-library/internal/store"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestToolsOverMCP(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	path := filepath.Join(t.TempDir(), "library.sqlite")
	writer, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.DB.Close()
	if err = writer.AddShelf(ctx, "Study", "One"); err != nil {
		t.Fatal(err)
	}
	shelves, _ := writer.Shelves(ctx)
	book, err := writer.Save(ctx, store.Input{Title: "The Code Book", Authors: []string{"Simon Singh"}, ShelfID: &shelves[0].ID, RequestKey: "mcp-test-book"})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.DB.Close()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := New(reader).Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	clientSession, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()
	tools, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 3 {
		t.Fatalf("tools: %+v", tools.Tools)
	}
	for _, tool := range tools.Tools {
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Fatalf("Tool not read-only: %s", tool.Name)
		}
	}
	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "search_books", Arguments: map[string]any{"query": "Singh", "available_only": true}})
	if err != nil || result.IsError {
		t.Fatalf("search: %v %+v", err, result)
	}
	raw, _ := json.Marshal(result.StructuredContent)
	var search SearchOutput
	if err = json.Unmarshal(raw, &search); err != nil {
		t.Fatal(err)
	}
	if search.Total != 1 || len(search.Books) != 1 || search.Books[0].ID != book.ID || search.Books[0].Copies[0].Shelf != "Study / One" {
		t.Fatalf("search %+v", search)
	}
	result, err = clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "get_book", Arguments: map[string]any{"id": book.ID}})
	if err != nil || result.IsError {
		t.Fatalf("get: %v %+v", err, result)
	}
	result, err = clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "list_shelves", Arguments: map[string]any{}})
	if err != nil || result.IsError {
		t.Fatalf("shelves: %v %+v", err, result)
	}
	for _, args := range []map[string]any{{"id": "bad"}, {"id": 0}, {"id": 999}, {}} {
		result, err = clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "get_book", Arguments: args})
		if err == nil && !result.IsError {
			t.Fatalf("invalid argument accepted: %v", args)
		}
	}
	if _, err = reader.DB.Exec("DELETE FROM books"); err == nil {
		t.Fatal("Read-only SQLite accepted deletion")
	}
	// Reading live WAL must reveal updates without needing to restart MCP.
	if _, err = writer.AddCopy(ctx, book.ID, nil, "mcp-new-copy"); err != nil {
		t.Fatal(err)
	}
	result, err = clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "get_book", Arguments: map[string]any{"id": book.ID}})
	if err != nil || result.IsError {
		t.Fatalf("live book: %v %+v", err, result)
	}
	raw, _ = json.Marshal(result.StructuredContent)
	var current store.Book
	json.Unmarshal(raw, &current)
	if current.Count != 2 {
		t.Fatalf("Live WAL count: %d", current.Count)
	}
}

func TestReadOnlyDoesNotCreateOrMigrate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.sqlite")
	if _, err := OpenReadOnly(path); err == nil {
		t.Fatal("Missing database accepted")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("Missing database created")
	}
	if err := os.WriteFile(path, []byte{}, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenReadOnly(path); err == nil {
		t.Fatal("Uninitialized database accepted")
	}
	info, _ := os.Stat(path)
	if info.Size() != 0 {
		t.Fatal("Database was migrated")
	}
}

func TestLegacyShelfLedgerHasFriendlyReadOnlyError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	path := filepath.Join(t.TempDir(), "library.sqlite")
	writer, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.DB.Close()
	if _, err = writer.DB.Exec("DROP TABLE location_versions"); err != nil {
		t.Fatal(err)
	}
	reader, err := OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.DB.Close()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := New(reader).Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	client, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	result, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "list_shelves", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(result)
	if !result.IsError || !strings.Contains(string(raw), "Start the updated library web application") || strings.Contains(string(raw), "SQL logic error") {
		t.Fatalf("Legacy result: %s", raw)
	}
	var count int
	if err = reader.DB.QueryRow("SELECT count(*) FROM sqlite_master WHERE name='location_versions'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("MCP migrated ledger: %d %v", count, err)
	}
}
