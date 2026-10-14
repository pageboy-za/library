package mcpserver

import (
	"context"
	"encoding/json"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"home-library/internal/catalogue"
	"home-library/internal/store"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDiscoveryProtocolAndOldDatabase(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	path := filepath.Join(t.TempDir(), "library.sqlite")
	writer, e := store.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer writer.DB.Close()
	book, e := writer.Save(ctx, store.Input{Title: "The Code Book", Authors: []string{"Simon Singh"}, RequestKey: "discover-test-book"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = writer.DB.Exec("DROP TABLE IF EXISTS book_discovery"); e != nil {
		t.Fatal(e)
	}
	reader, e := OpenReadOnly(path)
	if e != nil {
		t.Fatal(e)
	}
	defer reader.DB.Close()
	ct, st := mcp.NewInMemoryTransports()
	ss, e := New(reader, catalogue.DiscoveryService{Store: reader}).Connect(ctx, st, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer ss.Close()
	cs, e := mcp.NewClient(&mcp.Implementation{Name: "discovery-test", Version: "1"}, nil).Connect(ctx, ct, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer cs.Close()
	tools, e := cs.ListTools(ctx, nil)
	if e != nil || len(tools.Tools) != 4 {
		t.Fatalf("Four tools expected: %v %+v", e, tools)
	}
	result, e := cs.CallTool(ctx, &mcp.CallToolParams{Name: "discover_books", Arguments: map[string]any{"query": "cryptography"}})
	if e != nil || !result.IsError {
		t.Fatalf("Old database should fail without migration: %v %+v", e, result)
	}
	text, _ := json.Marshal(result)
	if !strings.Contains(string(text), "Start the updated library web application") {
		t.Fatal(string(text))
	}
	// Only the writer/web application may initialize the additive discovery table.
	if e = store.EnsureDiscoverySchema(writer); e != nil {
		t.Fatal(e)
	}
	if _, e = writer.SaveDiscovery(ctx, book.ID, store.Discovery{Tags: []string{"cryptography"}, Subjects: []string{"Codes"}, Description: "History of secret communication"}); e != nil {
		t.Fatal(e)
	}
	result, e = cs.CallTool(ctx, &mcp.CallToolParams{Name: "discover_books", Arguments: map[string]any{"query": "books about cryptography"}})
	if e != nil || result.IsError {
		t.Fatalf("Discovery failed: %v %+v", e, result)
	}
	raw, _ := json.Marshal(result.StructuredContent)
	var out DiscoverOutput
	if e = json.Unmarshal(raw, &out); e != nil {
		t.Fatal(e)
	}
	if len(out.Results) != 1 || out.Results[0].Book.ID != book.ID || len(out.Results[0].Reasons) != 1 || out.Results[0].Reasons[0] != "Matches your tag: cryptography" {
		t.Fatalf("Missing explanation: %+v", out)
	}
	if _, e = reader.DB.Exec("DROP TABLE book_discovery"); e == nil {
		t.Fatal("Discovery reader could write")
	}
}
