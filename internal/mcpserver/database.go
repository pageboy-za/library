package mcpserver

import (
	"database/sql"
	"fmt"
	"home-library/internal/store"
	"net/url"
	"os"
	"path/filepath"
)

// OpenReadOnly never creates or migrates a database. A read-only connection can
// observe the running web app's WAL, so immutable=1 must not be used here.
// The process has local operator access to this database, equivalent to staff
// catalogue access. It is not an anonymous HTTP/member endpoint.
func OpenReadOnly(path string) (*store.Store, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("Open existing library database: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("Library database must be a regular file")
	}
	uri := url.URL{Scheme: "file", Path: abs, RawQuery: "mode=ro&_pragma=query_only(1)&_pragma=busy_timeout(5000)"}
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	fail := func(e error) (*store.Store, error) { db.Close(); return nil, e }
	if err = db.Ping(); err != nil {
		return fail(err)
	}
	var version int
	if err = db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return fail(err)
	}
	// Schema v2 adds staff tables without changing the catalogue read model.
	if version != 1 && version != 2 {
		return fail(fmt.Errorf("Unsupported library schema %d; run the matching web application first", version))
	}
	return &store.Store{DB: db}, nil
}
