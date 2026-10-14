package bookpages

import (
	"context"
	"encoding/json"
	"home-library/internal/store"
	"path/filepath"
	"testing"
)

func TestSubtitleIncludedInStaticMetadata(t *testing.T) {
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.DB.Close()
	text := "A subtitle <script>escaped in HTML</script>"
	_, e = s.Save(context.Background(), store.Input{Title: "Static fixture", Subtitle: &text, RequestKey: "static-subtitle-save"})
	if e != nil {
		t.Fatal(e)
	}
	g, e := New(s, t.TempDir(), t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	raw, e := g.snapshot(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	var books []Metadata
	if e = json.Unmarshal(raw, &books); e != nil || len(books) != 1 || books[0].Subtitle != text {
		t.Fatal(string(raw), e)
	}
}
