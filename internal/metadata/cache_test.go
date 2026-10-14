package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type cacheTransport func(*http.Request) (*http.Response, error)

func (f cacheTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func cacheHTTP(body string, calls *int) *http.Client {
	return &http.Client{Transport: cacheTransport(func(r *http.Request) (*http.Response, error) {
		*calls++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
}

const cacheResponse = `{"ISBN:9780140328721":{"title":"Fantastic Mr. Fox","authors":[{"name":"Roald Dahl"}],"publish_date":"1988","identifiers":{"openlibrary":["OL123M"]}}}`

func TestPersistentCacheRestartCanonicalAndStaleOffline(t *testing.T) {
	dir := t.TempDir()
	calls := 0
	c := NewWithCache(dir)
	c.HTTP = cacheHTTP(cacheResponse, &calls)
	r, e := c.LookupDetails(context.Background(), "0140328726")
	if e != nil || r.Cached || r.FetchedAt.IsZero() || r.Draft.ISBN != "9780140328721" {
		t.Fatal(r, e)
	}
	path := filepath.Join(dir, "9780140328721.json")
	info, e := os.Stat(path)
	if e != nil || info.Mode().Perm() != 0600 {
		t.Fatal(info, e)
	}
	offline := NewWithCache(dir)
	offline.HTTP = &http.Client{Transport: cacheTransport(func(*http.Request) (*http.Response, error) {
		t.Fatal("fresh cache made network call")
		return nil, errors.New("offline")
	})}
	cached, e := offline.LookupDetails(context.Background(), "9780140328721")
	if e != nil || !cached.Cached || cached.Warning != "" || cached.Draft.Title != r.Draft.Title {
		t.Fatal(cached, e)
	}
	cached.Draft.Authors[0] = "changed"
	again, _ := offline.LookupDetails(context.Background(), "9780140328721")
	if again.Draft.Authors[0] != "Roald Dahl" {
		t.Fatal("caller mutated cache")
	}
	entry := cacheEntry{ISBN: r.Draft.ISBN, Draft: r.Draft, FetchedAt: time.Now().Add(-8 * 24 * time.Hour)}
	if e = c.writeCache(entry); e != nil {
		t.Fatal(e)
	}
	offline = NewWithCache(dir)
	offline.HTTP = &http.Client{Transport: cacheTransport(func(*http.Request) (*http.Response, error) { return nil, errors.New("offline") })}
	cached, e = offline.LookupDetails(context.Background(), r.Draft.ISBN)
	if e != nil || !cached.Cached || cached.Warning == "" || cached.FetchedAt.After(time.Now().Add(-7*24*time.Hour)) {
		t.Fatal(cached, e)
	}
}
func TestCacheRefreshFailurePreservesThenReplaces(t *testing.T) {
	dir := t.TempDir()
	calls := 0
	c := NewWithCache(dir)
	c.HTTP = cacheHTTP(cacheResponse, &calls)
	first, e := c.LookupDetails(context.Background(), "9780140328721")
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(dir, first.Draft.ISBN+".json")
	before, _ := os.ReadFile(path)
	c.last = time.Time{}
	c.HTTP = &http.Client{Transport: cacheTransport(func(*http.Request) (*http.Response, error) { return nil, errors.New("offline") })}
	if _, e = c.Refresh(context.Background(), first.Draft.ISBN); e == nil {
		t.Fatal("refresh failure concealed")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("failed refresh changed disk cache")
	}
	again, e := c.LookupDetails(context.Background(), first.Draft.ISBN)
	if e != nil || again.Draft.Title != first.Draft.Title {
		t.Fatal(again, e)
	}
	c.last = time.Time{}
	c.HTTP = cacheHTTP(strings.Replace(cacheResponse, "Fantastic Mr. Fox", "Updated title", 1), &calls)
	refreshed, e := c.Refresh(context.Background(), first.Draft.ISBN)
	if e != nil || refreshed.Cached || refreshed.Draft.Title != "Updated title" || calls != 2 {
		t.Fatal(refreshed, e, calls)
	}
}
func TestCacheInvalidInputCorruptionAndWriteFailure(t *testing.T) {
	dir := t.TempDir()
	calls := 0
	c := NewWithCache(dir)
	c.HTTP = cacheHTTP(cacheResponse, &calls)
	if _, e := c.LookupDetails(context.Background(), "../../123"); e == nil || calls != 0 {
		t.Fatal("invalid ISBN used")
	}
	path := filepath.Join(dir, "9780140328721.json")
	os.WriteFile(path, []byte("corrupted"), 0600)
	r, e := c.LookupDetails(context.Background(), "9780140328721")
	if e != nil || r.Cached || calls != 1 {
		t.Fatal(r, e, calls)
	}
	bad := cacheEntry{ISBN: "9780140328721", Draft: r.Draft, FetchedAt: time.Now().Add(24 * time.Hour)}
	raw, _ := json.Marshal(bad)
	os.WriteFile(path, raw, 0600)
	fresh := NewWithCache(dir)
	fresh.HTTP = cacheHTTP(cacheResponse, &calls)
	if r, e = fresh.LookupDetails(context.Background(), "9780140328721"); e != nil || r.Cached || calls != 2 {
		t.Fatal(r, e, calls)
	}
	blocking := filepath.Join(t.TempDir(), "file")
	os.WriteFile(blocking, []byte("leave intact"), 0600)
	broken := NewWithCache(blocking)
	broken.HTTP = cacheHTTP(cacheResponse, &calls)
	r, e = broken.LookupDetails(context.Background(), "9780140328721")
	if e != nil || r.Warning == "" || r.Draft.Title == "" {
		t.Fatal(r, e)
	}
	empty := NewWithCache(t.TempDir())
	empty.HTTP = cacheHTTP(strings.Replace(cacheResponse, "Fantastic Mr. Fox", "", 1), &calls)
	if _, e = empty.LookupDetails(context.Background(), "9780140328721"); e == nil {
		t.Fatal("empty provider title cached")
	}
	if len(empty.cache) != 0 {
		t.Fatal("unusable title retained")
	}
}

func TestCacheRejectsMismatchedAndOversizedFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "9780140328721.json")
	calls := 0
	for _, raw := range [][]byte{[]byte(strings.Repeat(" ", maxCacheBytes+1)), []byte(`{"isbn":"9781857028898","fetched_at":"2026-01-01T00:00:00Z","draft":{"isbn":"9780140328721","title":"Wrong book"}}`)} {
		os.WriteFile(path, raw, 0600)
		c := NewWithCache(dir)
		c.HTTP = cacheHTTP(cacheResponse, &calls)
		r, e := c.LookupDetails(context.Background(), "9780140328721")
		if e != nil || r.Cached || r.Draft.Title != "Fantastic Mr. Fox" {
			t.Fatal(r, e)
		}
	}
	if calls != 2 {
		t.Fatal(calls)
	}
}
