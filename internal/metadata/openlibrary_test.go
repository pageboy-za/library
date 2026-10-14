package metadata

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEditionLookupCacheAndFailure(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("bibkeys") != "ISBN:9780140328721" {
			t.Error(r.URL)
		}
		if r.Header.Get("User-Agent") == "" {
			t.Error("no client identity")
		}
		fmt.Fprint(w, `{"ISBN:9780140328721":{"title":"Matilda","subtitle":"A fixture subtitle","authors":[{"name":"Roald Dahl"}],"publishers":[{"name":"Puffin"}],"publish_date":"1988","url":"https://openlibrary.org/books/OL123M","identifiers":{"openlibrary":["OL123M"]}}}`)
	}))
	defer s.Close()
	c := New()
	c.Base = s.URL
	in, e := c.Lookup(context.Background(), "9780140328721")
	if e != nil || (in.Subtitle == nil || *in.Subtitle != "A fixture subtitle") || in.Publisher != "Puffin" || in.PublicationDate != "1988" || in.SourceID != "OL123M" {
		t.Fatal(in, e)
	}
	if _, e = c.Lookup(context.Background(), "9780140328721"); e != nil || calls != 1 {
		t.Fatal("not cached", calls, e)
	}
	s.Close()
	c = New()
	c.Base = s.URL
	if _, e = c.Lookup(context.Background(), "9780140328721"); e == nil {
		t.Fatal("failed lookup accepted")
	}
}
