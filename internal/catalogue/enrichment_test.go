package catalogue

import (
	"context"
	"errors"
	"home-library/internal/store"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestEnrichmentPreservesPrivateDataAndFollowsLinkedWork(t *testing.T) {
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.DB.Close()
	store.EnsureDiscoverySchema(s)
	ctx := context.Background()
	b, e := s.Save(ctx, store.Input{Title: "Test", SourceID: "OL123M", RequestKey: "enrich-book"})
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.SaveDiscovery(ctx, b.ID, store.Discovery{Tags: []string{"private"}, Notes: "private note", Description: "my description"})
	if e != nil {
		t.Fatal(e)
	}
	calls := 0
	client := mockEnrichmentHTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path == "/books/OL123M.json" {
			w.Write([]byte(`{"description":"upstream description","works":[{"key":"/works/OL99W"}]}`))
		} else if r.URL.Path == "/works/OL99W.json" {
			w.Write([]byte(`{"description":{"value":"work description"},"subjects":["Cryptography"]}`))
		} else {
			t.Error(r.URL.Path)
		}
	}))
	svc := NewEnrichment(s)
	svc.HTTP = client
	got, e := svc.Enrich(ctx, b.ID)
	if e != nil || !got.WorkLevel || !got.Changed || got.Discovery.Description != "my description" || got.Discovery.Tags[0] != "private" || got.Discovery.Notes != "private note" || got.Discovery.Subjects[0] != "Cryptography" {
		t.Fatalf("enrichment %+v %v", got, e)
	}
	_, e = svc.Enrich(ctx, b.ID)
	if e != nil || calls != 2 {
		t.Fatalf("already filled %d %v", calls, e)
	}
}
func TestEnrichmentConflictAndUnsafeWork(t *testing.T) {
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.DB.Close()
	store.EnsureDiscoverySchema(s)
	ctx := context.Background()
	b, e := s.Save(ctx, store.Input{Title: "Test", SourceID: "OL123M", RequestKey: "enrich-conflict"})
	if e != nil {
		t.Fatal(e)
	}
	client := mockEnrichmentHTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := s.SaveDiscovery(ctx, b.ID, store.Discovery{Notes: "concurrent edit"})
		if err != nil {
			t.Error(err)
		}
		w.Write([]byte(`{"description":"upstream","subjects":["Science"],"works":[{"key":"https://evil.test/works/OL1W"}]}`))
	}))
	svc := NewEnrichment(s)
	svc.HTTP = client
	if _, e = svc.Enrich(ctx, b.ID); !errors.Is(e, store.ErrConflict) {
		t.Fatalf("expected conflict %v", e)
	}
	d, _ := s.Discovery(ctx, b.ID)
	if d.Notes != "concurrent edit" || d.Description != "" {
		t.Fatal(d)
	}
	svc.last = time.Now()
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, e = svc.fetch(cancelled, "/books/OL123M"); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}

func TestEnrichmentRejectsUnsafeWorkAndRedirect(t *testing.T) {
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.DB.Close()
	store.EnsureDiscoverySchema(s)
	ctx := context.Background()
	b, e := s.Save(ctx, store.Input{Title: "Test", SourceID: "OL123M", RequestKey: "enrich-unsafe"})
	if e != nil {
		t.Fatal(e)
	}
	calls := 0
	client := mockEnrichmentHTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Write([]byte(`{"works":[{"key":"/works/OL9W?external=1"}]}`))
	}))
	svc := NewEnrichment(s)
	svc.HTTP = client
	got, e := svc.Enrich(ctx, b.ID)
	if e != nil || got.Changed || calls != 1 {
		t.Fatalf("unsafe work %+v %v calls %d", got, e, calls)
	}
	redirect := mockEnrichmentHTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://evil.invalid/books/OL9M.json", http.StatusFound)
	}))
	svc.HTTP = redirect
	svc.last = time.Time{}
	if _, e = svc.Enrich(ctx, b.ID); e == nil {
		t.Fatal("external redirect accepted")
	}
}

type enrichmentTransport func(*http.Request) (*http.Response, error)

func (f enrichmentTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func enrichmentResponse(body string) *http.Response {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func TestEnrichmentRejectsConcurrentEditionChange(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	ctx := context.Background()
	book, err := s.Save(ctx, store.Input{Title: "Old edition", ISBN: "9781857028898", SourceID: "OL123M", RequestKey: "edition-race"})
	if err != nil {
		t.Fatal(err)
	}
	before, err := s.SaveDiscovery(ctx, book.ID, store.Discovery{Notes: "Keep private note", Tags: []string{"private"}})
	if err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}, 1), make(chan struct{}, 1)
	defer close(release)
	svc := NewEnrichment(s)
	svc.HTTP = &http.Client{Transport: enrichmentTransport(func(r *http.Request) (*http.Response, error) {
		started <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
		return enrichmentResponse(`{"description":"Old edition description","subjects":["Old edition subject"]}`), nil
	})}
	finished := make(chan error, 1)
	go func() { _, err := svc.Enrich(ctx, book.ID); finished <- err }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("lookup did not start")
	}
	if _, err = s.Edit(ctx, book.ID, store.Input{Title: "New edition", ISBN: "9780140328721", Revision: book.Revision}); err != nil {
		t.Fatal(err)
	}
	release <- struct{}{}
	select {
	case err = <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("enrichment did not finish")
	}
	if !errors.Is(err, store.ErrConflict) {
		t.Fatal("old edition enrichment accepted", err)
	}
	actual, err := s.Discovery(ctx, book.ID)
	if err != nil || !reflect.DeepEqual(actual, before) {
		t.Fatal("edition conflict changed discovery", actual, err)
	}
}

func TestEnrichmentOnlyFetchesWorkForNeededFields(t *testing.T) {
	for _, fixture := range []struct {
		name        string
		local       store.Discovery
		body        string
		description string
		subject     string
	}{
		{"keep local subjects", store.Discovery{Subjects: []string{"Reviewed subject"}}, `{"description":"Edition description","works":[{"key":"/works/OL9W"}]}`, "Edition description", "Reviewed subject"},
		{"keep local description", store.Discovery{Description: "Reviewed description"}, `{"subjects":["Edition subject"],"works":[{"key":"/works/OL9W"}]}`, "Reviewed description", "Edition subject"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			s, err := store.Open(filepath.Join(t.TempDir(), "db"))
			if err != nil {
				t.Fatal(err)
			}
			defer s.DB.Close()
			ctx := context.Background()
			book, err := s.Save(ctx, store.Input{Title: "Fixture", SourceID: "OL123M", RequestKey: "needed-fields"})
			if err != nil {
				t.Fatal(err)
			}
			fixture.local.Notes = "Private note"
			fixture.local.Tags = []string{"private"}
			if _, err = s.SaveDiscovery(ctx, book.ID, fixture.local); err != nil {
				t.Fatal(err)
			}
			calls := 0
			svc := NewEnrichment(s)
			svc.HTTP = &http.Client{Transport: enrichmentTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Path != "/books/OL123M.json" {
					return nil, errors.New("linked work is offline")
				}
				return enrichmentResponse(fixture.body), nil
			})}
			got, err := svc.Enrich(ctx, book.ID)
			if err != nil || calls != 1 || !got.Changed || got.WorkLevel || got.Discovery.Description != fixture.description || len(got.Discovery.Subjects) != 1 || got.Discovery.Subjects[0] != fixture.subject || got.Discovery.Notes != "Private note" || got.Discovery.Tags[0] != "private" {
				t.Fatal("unnecessary work fallback", got, calls, err)
			}
		})
	}
}

// Serve provider fixtures entirely in memory; no listening ports or network.
func mockEnrichmentHTTP(handler http.HandlerFunc) *http.Client {
	return &http.Client{Transport: enrichmentTransport(func(request *http.Request) (*http.Response, error) {
		recorder := httptest.NewRecorder()
		handler(recorder, request)
		return recorder.Result(), nil
	})}
}
