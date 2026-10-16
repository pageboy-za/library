package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"home-library/internal/covers"
	"home-library/internal/store"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type recoveringPages struct {
	directory string
	fail      bool
}

func (p *recoveringPages) Directory() string { return p.directory }
func (p *recoveringPages) Ensure(context.Context) error {
	if p.fail {
		return errors.New("private build failure detail")
	}
	return nil
}

type blockingPages struct {
	directory string
	started   chan struct{}
	release   chan struct{}
	calls     atomic.Int32
	fail      bool
}

func (p *blockingPages) Directory() string { return p.directory }
func (p *blockingPages) Ensure(ctx context.Context) error {
	if p.calls.Add(1) == 1 {
		close(p.started)
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-p.release:
	}
	if p.fail {
		return errors.New("private build failure detail")
	}
	return nil
}
func waitBackground(t *testing.T, s *Server) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if s.ActiveWrites.Load() == 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("background work did not finish")
}
func TestSavesAndReadsDoNotWaitForStaticPublication(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(map[bool]string{false: "slow", true: "failed"}[failed], func(t *testing.T) {
			db, err := store.Open(filepath.Join(t.TempDir(), "db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.DB.Close()
			pages := &blockingPages{directory: t.TempDir(), started: make(chan struct{}), release: make(chan struct{}), fail: failed}
			os.MkdirAll(filepath.Join(pages.directory, "book-view"), 0700)
			os.WriteFile(filepath.Join(pages.directory, "book-view", "index.html"), []byte("Live book shell"), 0600)
			os.WriteFile(filepath.Join(pages.directory, "index.html"), []byte("Live catalogue shell"), 0600)
			lifetime, cancel := context.WithCancel(context.Background())
			defer cancel()
			server := &Server{Store: db, Pages: pages, Lifecycle: lifetime}
			h := server.testHandler(t)
			t.Cleanup(func() { cancel(); waitBackground(t, server) })
			call := func(method, path, body string) *httptest.ResponseRecorder {
				r := httptest.NewRequest(method, "http://localhost:8080"+path, strings.NewReader(body))
				r.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				done := make(chan struct{})
				go func() { h.ServeHTTP(w, r); close(done) }()
				select {
				case <-done:
				case <-time.After(time.Second):
					t.Fatal("request waited for blocked build", method, path)
				}
				return w
			}
			body := `{"title":"Immediate durable save","authors":["Example"],"request_key":"async-fixture"}`
			w := call("POST", "/api/books", body)
			if w.Code != 200 || strings.Contains(w.Body.String(), "page_warning") {
				t.Fatal(w.Code, w.Body.String())
			}
			var book store.Book
			if err = json.Unmarshal(w.Body.Bytes(), &book); err != nil || book.Count != 1 {
				t.Fatal(book, err)
			}
			select {
			case <-pages.started:
			case <-time.After(time.Second):
				t.Fatal("background build did not start")
			}
			if server.ActiveWrites.Load() != 1 {
				t.Fatal("updater cannot see background writes")
			}
			if w = call("GET", "/books/1/", ""); w.Code != 200 || w.Body.String() != "Live book shell" {
				t.Fatal(w.Code, w.Body.String())
			}
			if w = call("GET", "/", ""); w.Code != 200 || w.Body.String() != "Live catalogue shell" {
				t.Fatal(w.Code, w.Body.String())
			}
			for i := 0; i < 3; i++ {
				if w = call("POST", "/api/books", body); w.Code != 200 {
					t.Fatal(w.Code, w.Body.String())
				}
			}
			if w = call("PATCH", "/api/books/1", `{"title":"Freshly edited","authors":["Example"],"revision":1}`); w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			w = call("GET", "/api/books/1", "")
			if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Freshly edited") {
				t.Fatal(w.Code, w.Body.String())
			}
			stored, err := db.Book(context.Background(), book.ID)
			if err != nil || stored.Count != 1 {
				t.Fatal("retries changed inventory", stored, err)
			}
			close(pages.release)
			waitBackground(t, server)
			if pages.calls.Load() != 2 {
				t.Fatal("rapid saves were not coalesced", pages.calls.Load())
			}
			if w = call("GET", "/books/1/", ""); w.Code != 200 {
				t.Fatal("failed generation blocked book", w.Code)
			}
		})
	}
}

func TestBackgroundYieldsToUpdaterMaintenance(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.DB.Close()
	book, err := db.Save(context.Background(), store.Input{Title: "Maintenance fixture", RequestKey: "maintenance-background"})
	if err != nil {
		t.Fatal(err)
	}
	pages := &blockingPages{directory: t.TempDir(), started: make(chan struct{}), release: make(chan struct{})}
	server := &Server{Store: db, Pages: pages, MaintenancePath: filepath.Join(t.TempDir(), "maintenance")}
	server.queuePublication(book.ID)
	select {
	case <-pages.started:
	case <-time.After(time.Second):
		t.Fatal("worker not started")
	}
	server.queuePublication(book.ID)
	if err = os.WriteFile(server.MaintenancePath, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	close(pages.release)
	waitBackground(t, server)
	if pages.calls.Load() != 1 {
		t.Fatal("worker started more writes during maintenance")
	}
}

type slowCoverTransport struct{ started chan struct{} }

func (t slowCoverTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	close(t.started)
	<-r.Context().Done()
	return nil, r.Context().Err()
}
func TestSaveDoesNotWaitForCoverLookupAndShutdownCancelsIt(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.DB.Close()
	lifetime, cancel := context.WithCancel(context.Background())
	defer cancel()
	service := covers.New(db, t.TempDir())
	started := make(chan struct{})
	service.HTTP = &http.Client{Transport: slowCoverTransport{started}}
	server := &Server{Store: db, Covers: service, Lifecycle: lifetime}
	r := httptest.NewRequest("POST", "http://localhost:8080/api/books", strings.NewReader(`{"title":"Slow cover fixture","isbn":"9780140328721","authors":[],"request_key":"slow-cover"}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { server.testHandler(t).ServeHTTP(w, r); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("save blocked on cover lookup")
	}
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("background cover lookup did not start")
	}
	cancel()
	waitBackground(t, server)
	book, err := db.Book(context.Background(), 1)
	if err != nil || book.Count != 1 || book.Title != "Slow cover fixture" {
		t.Fatal("interrupted derived work lost the save", book, err)
	}
}
