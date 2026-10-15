package catalogue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"home-library/internal/library"
	"home-library/internal/store"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// EnrichmentService fetches only the saved edition and its explicitly linked work.
// Base is configurable for tests; HTTP routes never accept a remote URL.
type EnrichmentService struct {
	Store     *store.Store
	HTTP      *http.Client
	Base      string
	mu        sync.Mutex
	last      time.Time
	cache     map[string]enrichmentCacheEntry
	cacheHTTP *http.Client
	cacheBase string
}
type EnrichmentResult struct {
	Discovery    store.Discovery    `json:"discovery"`
	Bibliography store.Bibliography `json:"bibliography"`
	SourceURL    string             `json:"source_url"`
	WorkLevel    bool               `json:"work_level"`
	Changed      bool               `json:"changed"`
}

func NewEnrichment(s *store.Store) *EnrichmentService {
	return &EnrichmentService{Store: s, HTTP: &http.Client{Timeout: 12 * time.Second}, Base: "https://openlibrary.org"}
}

var editionID = regexp.MustCompile(`^OL[0-9]+M$`)
var workKey = regexp.MustCompile(`^/works/OL[0-9]+W$`)

type enrichmentRecord struct {
	Description json.RawMessage `json:"description"`
	Subjects    []string        `json:"subjects"`
	Works       []struct {
		Key string `json:"key"`
	} `json:"works"`
}

func description(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var value struct {
		Value string `json:"value"`
	}
	if json.Unmarshal(raw, &value) == nil {
		return value.Value
	}
	return ""
}

type enrichmentCacheEntry struct {
	Body    []byte
	Expires time.Time
}

func (s *EnrichmentService) fetch(ctx context.Context, path string) (enrichmentRecord, error) {
	body, err := s.fetchBytes(ctx, path+".json")
	if err != nil {
		return enrichmentRecord{}, err
	}
	var record enrichmentRecord
	err = json.Unmarshal(body, &record)
	return record, err
}
func (s *EnrichmentService) fetchBytes(ctx context.Context, path string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cacheHTTP != s.HTTP || s.cacheBase != s.Base {
		s.cache = nil
		s.cacheHTTP = s.HTTP
		s.cacheBase = s.Base
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if entry, ok := s.cache[path]; ok && time.Now().Before(entry.Expires) {
		return entry.Body, nil
	}
	if delay := time.Second - time.Since(s.last); delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	s.last = time.Now()
	req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(s.Base, "/")+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "HomeLibrary/0.1 (personal local catalogue)")
	client := *s.HTTP
	baseURL, _ := url.Parse(s.Base)
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if baseURL == nil || len(via) > 1 || next.URL.Scheme != baseURL.Scheme || next.URL.Host != baseURL.Host || !regexp.MustCompile(`^/books/OL[0-9]+M\.json$`).MatchString(next.URL.Path) || next.URL.RawQuery != "" {
			return errors.New("Unexpected Open Library redirect")
		}
		return nil
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("Open Library enrichment is unavailable. Your saved notes have not changed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Open Library enrichment returned status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(body) > 1<<20 {
		return nil, errors.New("Open Library response is too large")
	}
	if !json.Valid(body) {
		return nil, errors.New("Open Library returned invalid metadata")
	}
	if s.cache == nil || len(s.cache) >= 256 {
		s.cache = map[string]enrichmentCacheEntry{}
	}
	s.cache[path] = enrichmentCacheEntry{Body: body, Expires: time.Now().Add(time.Hour)}
	return body, nil
}
func (s *EnrichmentService) Enrich(ctx context.Context, id int64) (EnrichmentResult, error) {
	return s.EnrichWithOptions(ctx, id, false)
}
func (s *EnrichmentService) EnrichWithOptions(ctx context.Context, id int64, includeYear bool) (EnrichmentResult, error) {
	book, err := s.Store.Book(ctx, id)
	if err != nil {
		return EnrichmentResult{}, err
	}
	d, err := s.Store.Discovery(ctx, id)
	if err != nil {
		return EnrichmentResult{}, err
	}
	bibliography, err := s.Store.Bibliography(ctx, id)
	if err != nil {
		return EnrichmentResult{}, err
	}
	result := EnrichmentResult{Discovery: d, Bibliography: bibliography}
	needYear := includeYear && bibliography.FirstPublishedYear == nil && bibliography.Revision == 0
	if d.Description != "" && len(d.Subjects) > 0 && !needYear {
		return result, nil
	}
	path := ""
	if editionID.MatchString(book.SourceID) {
		path = "/books/" + book.SourceID
	} else if book.ISBN != "" {
		canonical, err := library.ISBN(book.ISBN)
		if err != nil {
			return result, err
		}
		path = "/isbn/" + canonical
	} else {
		return result, errors.New("This book needs an ISBN or an Open Library edition ID before enrichment")
	}
	record, err := s.fetch(ctx, path)
	if err != nil {
		return result, err
	}
	result.SourceURL = "https://openlibrary.org" + path
	desc := description(record.Description)
	subjects := record.Subjects
	if ((d.Description == "" && desc == "") || (len(d.Subjects) == 0 && len(subjects) == 0)) && len(record.Works) > 0 && workKey.MatchString(record.Works[0].Key) {
		work, err := s.fetch(ctx, record.Works[0].Key)
		if err != nil {
			return result, err
		}
		if d.Description == "" && desc == "" {
			desc = description(work.Description)
		}
		if len(d.Subjects) == 0 && len(subjects) == 0 {
			subjects = work.Subjects
		}
		result.WorkLevel = true
		result.SourceURL = "https://openlibrary.org" + record.Works[0].Key
	}
	if len(desc) > 20000 {
		desc = desc[:20000]
		for !utf8.ValidString(desc) {
			desc = desc[:len(desc)-1]
		}
	}
	if len(subjects) > 100 {
		subjects = subjects[:100]
	}
	labels := []string{}
	for _, label := range subjects {
		if len(label) <= 200 {
			labels = append(labels, label)
		}
	}
	if d.Description == "" && desc != "" {
		d.Description = desc
		result.Changed = true
	}
	if len(d.Subjects) == 0 && len(labels) > 0 {
		d.Subjects = labels
		result.Changed = true
	}
	discoveryChanged := result.Changed
	yearChanged := false
	if needYear && len(record.Works) > 0 && workKey.MatchString(record.Works[0].Key) {
		year, err := s.firstPublicationYear(ctx, record.Works[0].Key)
		if err != nil {
			return result, err
		}
		if year != nil {
			bibliography.FirstPublishedYear = year
			bibliography.SourceURL = "https://openlibrary.org" + record.Works[0].Key
			yearChanged = true
		}
	}
	if !discoveryChanged && !yearChanged {
		return result, nil
	}
	result.Discovery, result.Bibliography, err = s.Store.SaveEnrichment(ctx, id, d, bibliography, book.Revision, discoveryChanged, yearChanged)
	result.Changed = err == nil
	return result, err
}

// Search's first_publish_year describes the linked work, never its owned edition.
func (s *EnrichmentService) firstPublicationYear(ctx context.Context, key string) (*int, error) {
	if !workKey.MatchString(key) {
		return nil, errors.New("Invalid linked work")
	}
	query := url.Values{"q": {"key:" + key}, "fields": {"key,first_publish_year"}, "limit": {"1"}}
	body, err := s.fetchBytes(ctx, "/search.json?"+query.Encode())
	if err != nil {
		return nil, err
	}
	var response struct {
		Docs []struct {
			Key  string `json:"key"`
			Year *int   `json:"first_publish_year"`
		} `json:"docs"`
	}
	if err = json.Unmarshal(body, &response); err != nil {
		return nil, err
	}
	if len(response.Docs) == 1 && regexp.MustCompile(`^OL[0-9]+W$`).MatchString(response.Docs[0].Key) {
		response.Docs[0].Key = "/works/" + response.Docs[0].Key
	}
	if len(response.Docs) != 1 || response.Docs[0].Key != key {
		return nil, nil
	}
	year := response.Docs[0].Year
	if year != nil && (*year < 1 || *year > time.Now().Year()+1) {
		return nil, nil
	}
	return year, nil
}
