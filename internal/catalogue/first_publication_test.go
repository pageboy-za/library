package catalogue

import (
	"context"
	"errors"
	"home-library/internal/store"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFirstPublicationUsesLinkedWorkAndPreservesEdition(t *testing.T) {
	for _, bare := range []bool{false, true} {
		t.Run(map[bool]string{false: "path key", true: "bare key"}[bare], func(t *testing.T) {
			s, err := store.Open(filepath.Join(t.TempDir(), "db"))
			if err != nil {
				t.Fatal(err)
			}
			defer s.DB.Close()
			ctx := context.Background()
			b, err := s.Save(ctx, store.Input{Title: "Fixture", SourceID: "OL123M", PublicationDate: "2003", RequestKey: "first-publication"})
			if err != nil {
				t.Fatal(err)
			}
			_, err = s.SaveDiscovery(ctx, b.ID, store.Discovery{Description: "Reviewed description", Subjects: []string{"Reviewed subject"}, Notes: "Private fixture note", Tags: []string{"Private fixture tag"}})
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			svc := NewEnrichment(s)
			svc.HTTP = mockEnrichmentHTTP(func(w http.ResponseWriter, r *http.Request) {
				calls++
				switch r.URL.Path {
				case "/books/OL123M.json":
					w.Write([]byte(`{"publish_date":"2003","works":[{"key":"/works/OL99W"}]}`))
				case "/search.json":
					if r.URL.Query().Get("q") != "key:/works/OL99W" || r.URL.Query().Get("fields") != "key,first_publish_year" {
						t.Error(r.URL.String())
					}
					key := "/works/OL99W"
					if bare {
						key = "OL99W"
					}
					w.Write([]byte(`{"docs":[{"key":"` + key + `","first_publish_year":1978}]}`))
				default:
					t.Error("Unexpected request", r.URL.String())
				}
			})
			legacy, err := svc.Enrich(ctx, b.ID)
			if err != nil || legacy.Changed || calls != 0 {
				t.Fatal("legacy enrichment fetchedyear", legacy, calls, err)
			}
			result, err := svc.EnrichWithOptions(ctx, b.ID, true)
			if err != nil || !result.Changed || result.Bibliography.FirstPublishedYear == nil || *result.Bibliography.FirstPublishedYear != 1978 || result.Bibliography.SourceURL != "https://openlibrary.org/works/OL99W" || calls != 2 {
				t.Fatal(result, calls, err)
			}
			got, err := s.Book(ctx, b.ID)
			d, _ := s.Discovery(ctx, b.ID)
			if err != nil || got.PublicationDate != "2003" || got.Count != 1 || got.Revision != b.Revision || d.Description != "Reviewed description" || d.Subjects[0] != "Reviewed subject" || d.Notes != "Private fixture note" {
				t.Fatal(got, d, err)
			}
			if _, err = svc.EnrichWithOptions(ctx, b.ID, true); err != nil || calls != 2 {
				t.Fatal("alreadyfilledrefetched", calls, err)
			}
		})
	}
}
func TestFirstPublicationRejectsMismatchesAndFailuresAtomically(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		fail       bool
	}{{"mismatchedwork", `{"docs":[{"key":"/works/OL100W","first_publish_year":1988}]}`, false}, {"futureyear", `{"docs":[{"key":"/works/OL99W","first_publish_year":9999}]}`, false}, {"negativeyear", `{"docs":[{"key":"/works/OL99W","first_publish_year":-10}]}`, false}, {"missingyear", `{"docs":[{"key":"/works/OL99W"}]}`, false}, {"failedlookup", "", true}} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := store.Open(filepath.Join(t.TempDir(), "db"))
			if err != nil {
				t.Fatal(err)
			}
			defer s.DB.Close()
			ctx := context.Background()
			book, err := s.Save(ctx, store.Input{Title: "Fixture", SourceID: "OL123M", RequestKey: "firstyear-rejection"})
			if err != nil {
				t.Fatal(err)
			}
			svc := NewEnrichment(s)
			calls := 0
			svc.HTTP = &http.Client{Transport: enrichmentTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Path == "/books/OL123M.json" {
					return enrichmentResponse(`{"description":"Provider description","subjects":["Science"],"works":[{"key":"/works/OL99W"}]}`), nil
				}
				if r.URL.Path != "/search.json" {
					t.Fatal("unexpected path", r.URL.String())
				}
				if tc.fail {
					return nil, errors.New("offline")
				}
				return enrichmentResponse(tc.body), nil
			})}
			result, err := svc.EnrichWithOptions(ctx, book.ID, true)
			if (err != nil) != tc.fail {
				t.Fatal(result, err)
			}
			got, _ := s.Book(ctx, book.ID)
			d, _ := s.Discovery(ctx, book.ID)
			if got.FirstPublishedYear != nil || got.Count != 1 {
				t.Fatal(got)
			}
			if tc.fail && d.Description != "" {
				t.Fatal("partial update on network failure", d)
			}
			// A second missing-year request reuses bounded provider-response cache.
			if !tc.fail {
				if _, err = svc.EnrichWithOptions(ctx, book.ID, true); err != nil || calls != 2 {
					t.Fatal("cachemiss", calls, err)
				}
			}
		})
	}
}
func TestFirstPublicationConcurrentManualCorrection(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	ctx := context.Background()
	book, err := s.Save(ctx, store.Input{Title: "Fixture", SourceID: "OL123M", RequestKey: "firstyear-race"})
	if err != nil {
		t.Fatal(err)
	}
	svc := NewEnrichment(s)
	svc.HTTP = &http.Client{Transport: enrichmentTransport(func(r *http.Request) (*http.Response, error) {
		if strings.HasPrefix(r.URL.Path, "/books/") {
			return enrichmentResponse(`{"description":"Provider description","subjects":["Science"],"works":[{"key":"/works/OL99W"}]}`), nil
		}
		year := 1980
		if _, err := s.SaveBibliography(ctx, book.ID, store.Bibliography{FirstPublishedYear: &year}); err != nil {
			t.Error(err)
		}
		return enrichmentResponse(`{"docs":[{"key":"/works/OL99W","first_publish_year":1970}]}`), nil
	})}
	if _, err = svc.EnrichWithOptions(ctx, book.ID, true); !errors.Is(err, store.ErrConflict) {
		t.Fatal("staleimportaccepted", err)
	}
	got, _ := s.Book(ctx, book.ID)
	d, _ := s.Discovery(ctx, book.ID)
	if *got.FirstPublishedYear != 1980 || got.FirstPublishedYearSourceURL != "" || d.Description != "" {
		t.Fatal(got, d)
	}
	svc.last = time.Now()
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = svc.fetchBytes(cancelled, "/search.json?q=test"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestFirstPublicationRespectsManualClear(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	ctx := context.Background()
	book, err := s.Save(ctx, store.Input{Title: "Fixture", SourceID: "OL123M", RequestKey: "firstyear-manualclear"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveBibliography(ctx, book.ID, store.Bibliography{}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveDiscovery(ctx, book.ID, store.Discovery{Description: "Reviewed", Subjects: []string{"Science"}}); err != nil {
		t.Fatal(err)
	}
	svc := NewEnrichment(s)
	svc.HTTP = &http.Client{Transport: enrichmentTransport(func(r *http.Request) (*http.Response, error) {
		t.Error("manual clear caused lookup")
		return nil, errors.New("unexpected lookup")
	})}
	result, err := svc.EnrichWithOptions(ctx, book.ID, true)
	if err != nil || result.Changed || result.Bibliography.FirstPublishedYear != nil {
		t.Fatal(result, err)
	}
}
