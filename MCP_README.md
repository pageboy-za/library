# Local library MCP server

The browser and MCP read the same owned catalogue and stable numeric book IDs. This first version is deliberately read-only: there are no tools for creating books, changing copies, loans or uploading images, and no external metadata requests.

The local MCP process is a privileged, staff-equivalent reader: its OS user can read the database, including private catalogue information. Connect it only to trusted local clients. It does not use browser staff sessions and must not be exposed as an anonymous network service. Staff password hashes and session records are never returned by its tools.

## Build and run

From the repository root, with Go installed:

```sh
go build -o bin/library-mcp ./cmd/library-mcp
./bin/library-mcp -database /absolute/project/data/library.sqlite
```

The existing database must have been initialized by the web application. A missing or unsupported database fails with a message on standard error. It is never created or migrated by MCP.

The program speaks MCP over standard input/output; running it directly in a terminal waits for an MCP client rather than opening a webpage. Exit with Ctrl+C. Do not redirect application logs into standard output.

An example client configuration (adapt the enclosing configuration format to your chosen MCP client):

```json
{
  "mcpServers": {
    "home-library": {
      "command": "/absolute/project/bin/library-mcp",
      "args": [
        "-database",
        "/absolute/project/data/library.sqlite"
      ]
    }
  }
}
```

No client configuration is automatically installed. Nothing is exposed over a network port. The client you configure can read your local catalogue, including shelves and copy checkout flags. Discovery also searches private tags and notes and reports matching keywords, even though those fields are excluded from static HTML. Choose a client you intend to grant that access to.

## Available tools

| Tool | Arguments | Result |
| --- | --- | --- |
| `search_books` | Optional `query`, `shelf_id`, `available_only`, `page` | Owned books, total count, current page, page size 24 |
| `discover_books` | Required `query` theme or idea keywords | Up to 50 owned books with matching-field explanations and relevance scores |
| `get_book` | Required positive numeric `id` | Book details, physical copies, home shelves, checkout flags and available copy count |
| `list_shelves` | Empty object | Shelves with stable shelf/bookcase IDs, current bookcase labels and shelf revisions; empty bookcases are not listed |

For example, call `search_books` with `{"query":"Simon Singh"}`, then `get_book` with the returned book ID. ISBNs are searchable identifiers but are not the IDs accepted by `get_book`. Search follows the same catalogue implementation as the browser. `discover_books` uses the shared catalogue discovery service across subjects, descriptions, your tags and notes. It reports literal keyword matches with field-weighted ranking rather than semantic inference. For example, `{"query":"books about cryptography"}` can return a book tagged cryptography with the reason `Matches your tag: cryptography`.

An older database without the discovery table or location revision ledger returns a helpful error asking you to start the updated web application. MCP never initializes these tables.

Shelf values are the copy's home location. An `out: true` copy is currently checked out; this does not mean it is physically on its home shelf. Results do not invent related books outside your library.

## Architecture and checks

`internal/mcpserver.New` accepts a read-only `Catalogue` interface containing `Book`, `List` and `Shelves`. The CLI passes the existing store implementation plus `catalogue.DiscoveryService` through an optional `Discovery` interface. The original three tools remain available if no discovery service is supplied. SQLite opens using `mode=ro` plus `query_only`; it can read changes in the web application's live WAL and has no migration path. Do not use SQLite's `immutable=1` option against a running library, because that would prevent correctly observing WAL updates.

Tests initialize an MCP client and server with the SDK's in-memory transports, list and call tools, validate malformed and missing inputs, verify live database changes and reject SQL writes and accidental database creation.

```sh
go test -race ./internal/mcpserver ./cmd/library-mcp
```

Built with the [official MCP Go SDK v1.8.0](https://github.com/modelcontextprotocol/go-sdk/tree/v1.8.0). The [SDK server guide](https://github.com/modelcontextprotocol/go-sdk/blob/v1.8.0/docs/server.md) describes inferred argument schemas and tool validation; the [stdio example](https://github.com/modelcontextprotocol/go-sdk/blob/v1.8.0/examples/server/hello/main.go) demonstrates the transport used here.
