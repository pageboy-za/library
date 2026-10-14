package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"home-library/internal/store"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Client struct {
	HTTP     *http.Client
	Base     string
	mu       sync.Mutex
	last     time.Time
	cache    map[string]cacheEntry
	cacheDir string
}

func New() *Client { return NewWithCache("") }
func NewWithCache(dir string) *Client {
	return &Client{HTTP: &http.Client{Timeout: 12 * time.Second}, Base: "https://openlibrary.org", cache: map[string]cacheEntry{}, cacheDir: dir}
}
func (c *Client) Lookup(ctx context.Context, isbn string) (store.Input, error) {
	r, e := c.LookupDetails(ctx, isbn)
	return r.Draft, e
}

// fetch runs with c.mu held so requests to this provider remain paced.
func (c *Client) fetch(ctx context.Context, isbn string) (store.Input, error) {
	if delay := time.Second - time.Since(c.last); delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return store.Input{}, ctx.Err()
		}
	}
	c.last = time.Now()
	u := fmt.Sprintf("%s/api/books?bibkeys=ISBN:%s&jscmd=data&format=json", c.Base, isbn)
	req, e := http.NewRequestWithContext(ctx, "GET", u, nil)
	if e != nil {
		return store.Input{}, e
	}
	req.Header.Set("User-Agent", "HomeLibrary/0.1 (personal local catalogue)")
	resp, e := c.HTTP.Do(req)
	if e != nil {
		return store.Input{}, errors.New("Book lookup unavailable. Enter the details manually")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return store.Input{}, errors.New("Book lookup unavailable. Enter the details manually")
	}
	var data map[string]struct {
		Title    string `json:"title"`
		Subtitle string `json:"subtitle"`
		Authors  []struct {
			Name string `json:"name"`
		} `json:"authors"`
		Publishers []struct {
			Name string `json:"name"`
		} `json:"publishers"`
		Date        string `json:"publish_date"`
		URL         string `json:"url"`
		Identifiers struct {
			OL []string `json:"openlibrary"`
		} `json:"identifiers"`
	}
	raw, e := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if e != nil {
		return store.Input{}, e
	}
	if len(raw) > 1<<20 {
		return store.Input{}, errors.New("Book lookup returned too much data. Enter the details manually")
	}
	if e = json.Unmarshal(raw, &data); e != nil {
		return store.Input{}, e
	}
	v, ok := data["ISBN:"+isbn]
	if !ok {
		return store.Input{}, errors.New("No matching edition found. Enter the details manually")
	}
	subtitle := strings.TrimSpace(v.Subtitle)
	if len(subtitle) > 1000 {
		subtitle = ""
	}
	in := store.Input{ISBN: isbn, Title: v.Title, Subtitle: &subtitle, Authors: []string{}, PublicationDate: v.Date, SourceURL: v.URL}
	for _, a := range v.Authors {
		in.Authors = append(in.Authors, a.Name)
	}
	if len(v.Publishers) > 0 {
		in.Publisher = v.Publishers[0].Name
	}
	if len(v.Identifiers.OL) > 0 {
		in.SourceID = v.Identifiers.OL[0]
	}
	if strings.TrimSpace(in.Title) == "" {
		return store.Input{}, errors.New("No usable title found. Enter the details manually")
	}
	return in, nil
}
