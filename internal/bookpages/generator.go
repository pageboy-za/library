// Package bookpages publishes Astro-generated HTML from SQLite metadata snapshots.
package bookpages

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"home-library/internal/metrics"
	"home-library/internal/store"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Metadata struct {
	ID              int64    `json:"id"`
	ISBN            string   `json:"isbn"`
	Title           string   `json:"title"`
	Authors         []string `json:"authors"`
	Publisher       string   `json:"publisher"`
	PublicationDate string   `json:"publication_date"`
	CoverURL        string   `json:"cover_url"`
	Revision        int      `json:"revision"`
	Description     string   `json:"description"`
	Subjects        []string `json:"subjects"`
}

// Runner is replaceable in tests; production uses the locally installed Astro CLI.
type Runner func(context.Context, string, string) error
type Generator struct {
	Store *store.Store
	Web   string
	Root  string
	Run   Runner
	// Lifecycle is set before first use; shutdown cancels builds independently of browser requests.
	Lifecycle   context.Context
	mu          sync.Mutex
	fingerprint string
}

func New(s *store.Store, web, root string) (*Generator, error) {
	web, e := filepath.Abs(web)
	if e != nil {
		return nil, e
	}
	root, e = filepath.Abs(root)
	if e != nil {
		return nil, e
	}
	return &Generator{Store: s, Web: web, Root: root}, nil
}
func (g *Generator) Directory() string       { return filepath.Join(g.Root, "current") }
func (g *Generator) AssetsDirectory() string { return filepath.Join(g.Root, "assets") }

// Astro's fingerprinted assets outlive individual HTML generations. A tab may
// request an island bundle after a newer frontend build has been published.
func (g *Generator) preserveAssets(output string) error {
	root := filepath.Join(output, "_astro")
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("non-file Astro asset: %s", entry.Name())
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(g.AssetsDirectory(), relative)
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		old, err := os.ReadFile(destination)
		if err == nil {
			if sha256.Sum256(old) != sha256.Sum256(data) {
				return fmt.Errorf("Astro asset changed without a new fingerprint: %s", relative)
			}
			return nil
		}
		if !os.IsNotExist(err) {
			return err
		}
		if err = os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
			return err
		}
		temporary, err := os.CreateTemp(filepath.Dir(destination), ".asset-")
		if err != nil {
			return err
		}
		name := temporary.Name()
		defer os.Remove(name)
		if _, err = temporary.Write(data); err != nil {
			temporary.Close()
			return err
		}
		if err = temporary.Close(); err != nil {
			return err
		}
		return os.Rename(name, destination)
	})
}

// Keep the current generation, two predecessors and a one-hour grace period for
// in-flight readers. Only publisher-created directories are maintenance targets.
func (g *Generator) pruneGenerations(now time.Time) error {
	current, err := os.Readlink(g.Directory())
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(g.Root)
	if err != nil {
		return err
	}
	type generation struct {
		path     string
		modified time.Time
	}
	var candidates []generation
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "generation-") {
			continue
		}
		path := filepath.Join(g.Root, entry.Name())
		if filepath.Join(path, "site") == current {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		candidates = append(candidates, generation{path, info.ModTime()})
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].modified.After(candidates[j].modified) })
	for index, candidate := range candidates {
		if index < 2 || now.Sub(candidate.modified) < time.Hour {
			continue
		}
		if err := os.RemoveAll(candidate.path); err != nil {
			return err
		}
	}
	return nil
}

func (g *Generator) snapshot(ctx context.Context) ([]byte, error) {
	rows, e := g.Store.DB.QueryContext(ctx, "SELECT b.id,b.isbn,b.title,b.authors,b.publisher,b.publication_date,COALESCE('/covers/'||c.local_path,''),b.revision,COALESCE(d.description,''),COALESCE(d.subjects,'[]') FROM books b LEFT JOIN cover_assets c ON c.id=b.selected_cover_asset_id LEFT JOIN book_discovery d ON d.book_id=b.id ORDER BY b.id")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	books := []Metadata{}
	for rows.Next() {
		var b Metadata
		var authors, subjects string
		var isbn, cover *string
		if e = rows.Scan(&b.ID, &isbn, &b.Title, &authors, &b.Publisher, &b.PublicationDate, &cover, &b.Revision, &b.Description, &subjects); e != nil {
			return nil, e
		}
		if isbn != nil {
			b.ISBN = *isbn
		}
		if cover != nil {
			b.CoverURL = *cover
		}
		if e = json.Unmarshal([]byte(authors), &b.Authors); e != nil {
			return nil, e
		}
		if b.Authors == nil {
			b.Authors = []string{}
		}
		if e = json.Unmarshal([]byte(subjects), &b.Subjects); e != nil {
			return nil, e
		}
		books = append(books, b)
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	return json.Marshal(books)
}

// Ensure serializes builds, snapshots current committed metadata, and only publishes
// complete output. Copy/shelf/loan changes do not change the snapshot or trigger builds.
func (g *Generator) Ensure(ctx context.Context) error {
	waitStarted := time.Now()
	g.mu.Lock()
	metrics.Record(ctx, "publication_wait", time.Since(waitStarted), nil)
	defer g.mu.Unlock()
	snapshotStarted := time.Now()
	data, e := g.snapshot(ctx)
	metrics.Record(ctx, "snapshot", time.Since(snapshotStarted), e)
	if e != nil {
		return e
	}
	hash := sha256.Sum256(data)
	stamp := hex.EncodeToString(hash[:])
	if stamp == g.fingerprint {
		return nil
	}
	if e = os.MkdirAll(g.Root, 0700); e != nil {
		return e
	}
	// Seed the shared asset store from the live site during the first upgrade,
	// before any build can replace it or fail and leave fallback HTML in use.
	if e = g.preserveAssets(g.Directory()); e != nil {
		return fmt.Errorf("preserve live Astro assets: %w", e)
	}
	stage, e := os.MkdirTemp(g.Root, "generation-")
	if e != nil {
		return e
	}
	published := false
	defer func() {
		if !published {
			os.RemoveAll(stage)
		}
	}()
	snapshot := filepath.Join(stage, "books.json")
	if e = os.WriteFile(snapshot, data, 0600); e != nil {
		return e
	}
	output := filepath.Join(stage, "site")
	lifecycle := g.Lifecycle
	if lifecycle == nil {
		lifecycle = context.Background()
	}
	buildCtx, cancel := context.WithTimeout(lifecycle, 2*time.Minute)
	defer cancel()
	run := g.Run
	if run == nil {
		run = g.build
	}
	if e = metrics.Measure(ctx, "astro_build", func() error { return run(buildCtx, snapshot, output) }); e != nil {
		return fmt.Errorf("generate Astro book pages: %w", e)
	}
	if e = buildCtx.Err(); e != nil {
		return fmt.Errorf("page generation stopped: %w", e)
	}
	if _, e = os.Stat(filepath.Join(output, "index.html")); e != nil {
		return fmt.Errorf("incomplete Astro output: %w", e)
	}
	var books []Metadata
	if e = json.Unmarshal(data, &books); e != nil {
		return e
	}
	for _, b := range books {
		if _, e = os.Stat(filepath.Join(output, "books", fmt.Sprint(b.ID), "index.html")); e != nil {
			return fmt.Errorf("missing generated book %d: %w", b.ID, e)
		}
	}
	if e = g.preserveAssets(output); e != nil {
		return fmt.Errorf("preserve Astro assets: %w", e)
	}
	link := filepath.Join(g.Root, "next")
	// This temporary link is private to the serialized publisher.
	os.Remove(link)
	if e = os.Symlink(output, link); e != nil {
		return e
	}
	if e = metrics.Measure(ctx, "atomic_publish", func() error { return os.Rename(link, g.Directory()) }); e != nil {
		os.Remove(link)
		return e
	}
	published = true
	g.fingerprint = stamp
	if e = g.pruneGenerations(time.Now()); e != nil {
		log.Printf("Generated-page maintenance: %v", e)
	}
	return nil
}
func (g *Generator) build(ctx context.Context, snapshot, output string) error {
	cli := filepath.Join(g.Web, "node_modules", "astro", "bin", "astro.mjs")
	cmd := exec.CommandContext(ctx, "node", cli, "build", "--outDir", output)
	cmd.Dir = g.Web
	cmd.Env = append(os.Environ(), "LIBRARY_BOOK_SNAPSHOT="+snapshot, "ASTRO_TELEMETRY_DISABLED=1")
	log, e := cmd.CombinedOutput()
	if e != nil {
		return fmt.Errorf("install Node and run npm ci in %s; %w\n%s", g.Web, e, log)
	}
	return nil
}
