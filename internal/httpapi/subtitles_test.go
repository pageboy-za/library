package httpapi

import (
	"context"
	"encoding/json"
	"home-library/internal/store"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestSubtitleBatchCommitAndPublicationRecovery(t *testing.T) {
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.DB.Close()
	b, e := s.Save(context.Background(), store.Input{Title: "Subtitle fixture", RequestKey: "subtitle-http-save"})
	if e != nil {
		t.Fatal(e)
	}
	p := &recoveringPages{directory: t.TempDir(), fail: true}
	h := (&Server{Store: s, Pages: p}).Handler()
	call := func(path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "http://localhost:8080"+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	raw, _ := json.Marshal(map[string]any{"updates": []store.SubtitleUpdate{{ID: b.ID, Revision: b.Revision, Subtitle: "Saved despite build failure"}}})
	if w := call("/api/subtitles/backfill", string(raw)); w.Code != 200 || !strings.Contains(w.Body.String(), `"changed":1`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := call("/api/subtitles/publish", "{}"); w.Code != 202 || !strings.Contains(w.Body.String(), "page_warning") {
		t.Fatal(w.Code, w.Body.String())
	}
	got, e := s.Book(context.Background(), b.ID)
	if e != nil || got.Subtitle != "Saved despite build failure" || got.Count != 1 {
		t.Fatal(got, e)
	}
	if w := call("/api/subtitles/backfill", string(raw)); w.Code != 200 || !strings.Contains(w.Body.String(), `"skipped":1`) {
		t.Fatal(w.Code, w.Body.String())
	}
	p.fail = false
	if w := call("/api/subtitles/publish", "{}"); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := call("/api/subtitles/backfill", `{"updates":[{"id":1,"revision":1,"subtitle":"","unexpected":true}]}`); w.Code != 400 {
		t.Fatal(w.Code)
	}
}
