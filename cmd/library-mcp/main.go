// library-mcp runs a local, read-only MCP server over standard input/output.
package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"home-library/internal/catalogue"
	"home-library/internal/mcpserver"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	database := flag.String("database", "data/library.sqlite", "existing library SQLite file (never created or migrated)")
	flag.Parse()
	s, err := mcpserver.OpenReadOnly(*database)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer s.DB.Close()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err = mcpserver.New(s, catalogue.DiscoveryService{Store: s}).Run(ctx, &mcp.StdioTransport{}); err != nil && ctx.Err() == nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
