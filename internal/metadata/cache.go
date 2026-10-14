package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"home-library/internal/library"
	"home-library/internal/store"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const cacheFreshFor = 7 * 24 * time.Hour
const maxCacheBytes = 128 << 10

type cacheEntry struct {
	ISBN      string      `json:"isbn"`
	FetchedAt time.Time   `json:"fetched_at"`
	Draft     store.Input `json:"draft"`
}
type Result struct {
	Draft     store.Input `json:"draft"`
	FetchedAt time.Time   `json:"fetched_at"`
	Cached    bool        `json:"cached"`
	Warning   string      `json:"warning,omitempty"`
}

func cloneDraft(d store.Input) store.Input { d.Authors = append([]string{}, d.Authors...); return d }
func validEntry(e cacheEntry, isbn string, now time.Time) bool {
	if e.ISBN != isbn || e.Draft.ISBN != isbn || e.FetchedAt.IsZero() || e.FetchedAt.After(now.Add(5*time.Minute)) {
		return false
	}
	if strings.TrimSpace(e.Draft.Title) == "" || len(e.Draft.Title) > 500 || len(e.Draft.Authors) > 100 {
		return false
	}
	for _, a := range e.Draft.Authors {
		if len(a) > 500 {
			return false
		}
	}
	return e.Draft.ShelfID == nil && e.Draft.Revision == 0 && e.Draft.RequestKey == ""
}
func (c *Client) readCache(isbn string, now time.Time) (cacheEntry, bool) {
	if e, ok := c.cache[isbn]; ok && validEntry(e, isbn, now) {
		return e, true
	}
	if c.cacheDir == "" {
		return cacheEntry{}, false
	}
	f, err := os.Open(filepath.Join(c.cacheDir, isbn+".json"))
	if err != nil {
		return cacheEntry{}, false
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxCacheBytes+1))
	if err != nil || len(raw) > maxCacheBytes {
		return cacheEntry{}, false
	}
	var e cacheEntry
	if json.Unmarshal(raw, &e) != nil || !validEntry(e, isbn, now) {
		return cacheEntry{}, false
	}
	c.cache[isbn] = e
	return e, true
}
func (c *Client) writeCache(e cacheEntry) error {
	if c.cacheDir == "" {
		return nil
	}
	raw, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if len(raw) > maxCacheBytes {
		return errors.New("metadata cache entry is too large")
	}
	if err = os.MkdirAll(c.cacheDir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(c.cacheDir, ".metadata-")
	if err != nil {
		return err
	}
	path := f.Name()
	defer os.Remove(path)
	if err = f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err = f.Write(raw); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(path, filepath.Join(c.cacheDir, e.ISBN+".json")); err != nil {
		return err
	}
	dir, err := os.Open(c.cacheDir)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
func (c *Client) LookupDetails(ctx context.Context, rawISBN string) (Result, error) {
	return c.lookup(ctx, rawISBN, false)
}

// Refresh bypasses fresh/stale caches but never edits a saved catalogue record.
func (c *Client) Refresh(ctx context.Context, rawISBN string) (Result, error) {
	return c.lookup(ctx, rawISBN, true)
}
func (c *Client) lookup(ctx context.Context, rawISBN string, refresh bool) (Result, error) {
	isbn, err := library.ISBN(rawISBN)
	if err != nil {
		return Result{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cache == nil {
		c.cache = map[string]cacheEntry{}
	}
	now := time.Now().UTC()
	cached, exists := c.readCache(isbn, now)
	if exists && !refresh && now.Sub(cached.FetchedAt) < cacheFreshFor {
		return Result{Draft: cloneDraft(cached.Draft), FetchedAt: cached.FetchedAt, Cached: true}, nil
	}
	draft, err := c.fetch(ctx, isbn)
	if err != nil {
		if exists && !refresh && ctx.Err() == nil {
			return Result{Draft: cloneDraft(cached.Draft), FetchedAt: cached.FetchedAt, Cached: true, Warning: fmt.Sprintf("Using details cached on %s because Open Library could not refresh them. Review before saving.", cached.FetchedAt.Format("2006-01-02"))}, nil
		}
		return Result{}, err
	}
	entry := cacheEntry{ISBN: isbn, FetchedAt: time.Now().UTC(), Draft: cloneDraft(draft)}
	if !validEntry(entry, isbn, entry.FetchedAt) {
		return Result{}, errors.New("Open Library returned unusable details. Enter the details manually")
	}
	c.cache[isbn] = entry
	result := Result{Draft: cloneDraft(draft), FetchedAt: entry.FetchedAt}
	if err = c.writeCache(entry); err != nil {
		result.Warning = "Book details loaded, but the local cache could not be saved. They may need another lookup after restart."
	}
	return result, nil
}
