package covers

import (
	"bytes"
	"context"
	"errors"
	"home-library/internal/store"
	"image"
	"image/color"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestProviderDownloadSanitizedAndDoesNotMutateBook(t *testing.T) {
	s, id := setup(t)
	ctx := context.Background()
	raw := cameraJPEG(t, 6, 4, 8)
	a, e := s.save(ctx, store.CoverAsset{BookID: id, SourceKind: "openlibrary", SourceEditionID: "OL123M"}, raw, true, false)
	if e != nil {
		t.Fatal(e)
	}
	before, _ := s.Store.Book(ctx, id)
	out, e := s.Download(ctx, id, a.ID)
	if e != nil {
		t.Fatal(e)
	}
	cfg, format, e := image.DecodeConfig(bytes.NewReader(out))
	if e != nil || format != "jpeg" || cfg.Width != 8 || cfg.Height != 4 || bytes.Contains(out, []byte("Exif")) || bytes.Contains(out, []byte("GPS")) {
		t.Fatal(cfg, format, e)
	}
	after, _ := s.Store.Book(ctx, id)
	if before.ISBN != after.ISBN || before.Count != after.Count || before.Revision != after.Revision || before.CoverURL != after.CoverURL {
		t.Fatal("download mutated inventory or selection")
	}
	b, e := s.Store.Save(ctx, store.Input{Title: "Other", Authors: []string{}, RequestKey: "other-book-token"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Download(ctx, b.ID, a.ID); !errors.Is(e, store.ErrNotFound) {
		t.Fatal("cross-book download permitted", e)
	}
	mux := http.NewServeMux()
	s.Register(mux, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/books/"+strconv.FormatInt(id, 10)+"/covers/"+strconv.FormatInt(a.ID, 10)+"/download", nil))
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/jpeg" || w.Header().Get("Content-Disposition") != `attachment; filename="book-1-cover.jpg"` {
		t.Fatal(w.Code, w.Header())
	}
}
func TestPublicationWarningDoesNotExposeBuildLogs(t *testing.T) {
	s, id := setup(t)
	a, e := s.Upload(context.Background(), id, testImage(color.White))
	if e != nil {
		t.Fatal(e)
	}
	mux := http.NewServeMux()
	s.Register(mux, func(context.Context) error { return errors.New("/private/secret/node build log") })
	req := httptest.NewRequest("POST", "/api/books/1/covers/select", strings.NewReader(`{"asset_id":`+strconv.FormatInt(a.ID, 10)+`}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != 200 || strings.Contains(w.Body.String(), "secret") || strings.Contains(w.Body.String(), "build log") || !strings.Contains(w.Body.String(), "Cover saved") {
		t.Fatal(w.Code, w.Body.String())
	}
}
