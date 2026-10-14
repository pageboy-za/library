// Package mcpserver exposes the local owned catalogue through read-only MCP tools.
package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"home-library/internal/catalogue"
	"home-library/internal/store"
	"strings"
)

// Catalogue is shared with browser services; no mutation methods are exposed.
type Catalogue interface {
	Book(context.Context, int64) (store.Book, error)
	List(context.Context, string, int64, string, int) ([]store.Book, int, error)
	Shelves(context.Context) ([]store.Shelf, error)
}
type SearchInput struct {
	Query         string `json:"query,omitempty" jsonschema:"Search owned books by title author or ISBN"`
	ShelfID       int64  `json:"shelf_id,omitempty" jsonschema:"Optional shelf ID"`
	AvailableOnly bool   `json:"available_only,omitempty" jsonschema:"Require at least one available copy"`
	Page          int    `json:"page,omitempty" jsonschema:"Page number starting at 1; 24 results per page"`
}
type SearchOutput struct {
	Books    []store.Book `json:"books"`
	Total    int          `json:"total"`
	Page     int          `json:"page"`
	PageSize int          `json:"page_size"`
}
type BookInput struct {
	ID int64 `json:"id" jsonschema:"Stable owned book ID; not an ISBN"`
}
type ShelfOutput struct {
	Shelves []store.Shelf `json:"shelves"`
}

type Discovery interface {
	Search(context.Context, string) ([]catalogue.DiscoveryResult, error)
}
type DiscoverInput struct {
	Query string `json:"query" jsonschema:"Theme or idea keywords to match owned books; literal matching rather than semantic inference"`
}
type DiscoverOutput struct {
	Results []catalogue.DiscoveryResult `json:"results"`
	Method  string                      `json:"method"`
}

func New(c Catalogue, discovery ...Discovery) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "home-library", Version: "0.1.0"}, nil)
	closed := false
	annotations := &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: &closed}
	mcp.AddTool(s, &mcp.Tool{Name: "search_books", Description: "Search only books owned in this home library. Results include stable IDs, copies, home shelves, and checkout status. No external book lookups.", Annotations: annotations}, func(ctx context.Context, _ *mcp.CallToolRequest, in SearchInput) (*mcp.CallToolResult, SearchOutput, error) {
		if len(in.Query) > 500 || in.ShelfID < 0 || in.Page < 0 || in.Page > 100000 {
			return nil, SearchOutput{}, errors.New("Invalid query, shelf ID, or page")
		}
		if in.Page == 0 {
			in.Page = 1
		}
		availability := ""
		if in.AvailableOnly {
			availability = "available"
		}
		books, total, err := c.List(ctx, in.Query, in.ShelfID, availability, in.Page)
		return nil, SearchOutput{Books: books, Total: total, Page: in.Page, PageSize: 24}, err
	})
	mcp.AddTool(s, &mcp.Tool{Name: "get_book", Description: "Read an owned book by stable book ID, including its physical copies, shelves and checkout status.", Annotations: annotations}, func(ctx context.Context, _ *mcp.CallToolRequest, in BookInput) (*mcp.CallToolResult, store.Book, error) {
		if in.ID <= 0 {
			return nil, store.Book{}, fmt.Errorf("Book ID must be positive")
		}
		b, err := c.Book(ctx, in.ID)
		return nil, b, err
	})
	mcp.AddTool(s, &mcp.Tool{Name: "list_shelves", Description: "List local bookcases and shelves with their stable IDs.", Annotations: annotations}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, ShelfOutput, error) {
		sh, err := c.Shelves(ctx)
		if err != nil && strings.Contains(err.Error(), "no such table") {
			err = errors.New("Shelf revisions are unavailable in this database. Start the updated library web application once to initialize locations; MCP cannot migrate it")
		}
		return nil, ShelfOutput{Shelves: sh}, err
	})
	if len(discovery) > 0 && discovery[0] != nil {
		mcp.AddTool(s, &mcp.Tool{Name: "discover_books", Description: "Find owned books by theme or idea keywords across descriptions, subjects, your tags and notes. Each result explains its literal matches. Not semantic search and does not invent books you do not own.", Annotations: annotations}, func(ctx context.Context, _ *mcp.CallToolRequest, in DiscoverInput) (*mcp.CallToolResult, DiscoverOutput, error) {
			if strings.TrimSpace(in.Query) == "" || len(in.Query) > 500 {
				return nil, DiscoverOutput{}, errors.New("Enter discovery keywords under 500 characters")
			}
			results, err := discovery[0].Search(ctx, in.Query)
			if err != nil && strings.Contains(err.Error(), "no such table") {
				err = errors.New("Discovery is unavailable in this database. Start the updated library web application once to initialize discovery; MCP cannot migrate it")
			}
			return nil, DiscoverOutput{Results: results, Method: "literal keyword matches with field-weighted ranking"}, err
		})
	}
	return s
}
