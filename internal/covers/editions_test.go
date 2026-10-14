package covers

import (
	"bytes"
	"context"
	"image/color"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func editionTransport(s *Service, t *testing.T) transportFunc {
	return func(r *http.Request) (*http.Response, error) {
		s.last = time.Now().Add(-time.Second)
		var body []byte
		switch {
		case r.URL.Path == "/books/OL123M.json":
			body = []byte(`{"key":"/books/OL123M","works":[{"key":"/works/OL100W"}]}`)
		case r.URL.Path == "/works/OL100W/editions.json":
			if r.URL.Query().Get("limit") != "10" {
				t.Fatal("unbounded editions request")
			}
			body = []byte(`{"size":24,"entries":[{"key":"/books/OL456M","title":"Same work","publishers":["Other publisher"],"publish_date":"1999","isbn_13":["9781857028898"],"physical_format":"Paperback","languages":[{"key":"/languages/eng"}],"covers":[789]}]}`)
		case r.URL.Path == "/books/OL456M.json":
			body = []byte(`{"key":"/books/OL456M","works":[{"key":"/works/OL100W"}],"covers":[789]}`)
		case r.URL.Path == "/books/OL999M.json":
			body = []byte(`{"key":"/books/OL999M","works":[{"key":"/works/OL200W"}],"covers":[666]}`)
		case r.URL.Host == "covers.openlibrary.org":
			body = testImage(color.White)
		default:
			t.Fatal("unexpected URL", r.URL)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(body)), Header: http.Header{}}, nil
	}
}
func TestLinkedEditionBrowseAndPreviewPreservesBook(t *testing.T) {
	s, id := setup(t)
	ctx := context.Background()
	s.HTTP = &http.Client{Transport: editionTransport(s, t)}
	owned, _ := s.Store.Book(ctx, id)
	page, e := s.Editions(ctx, id, 1)
	if e != nil || len(page.Editions) != 1 || !page.HasMore || page.Editions[0].ISBN != "9781857028898" || !page.Editions[0].HasCover {
		t.Fatal(page, e)
	}
	assets, e := s.PreviewEdition(ctx, id, "OL456M")
	if e != nil || len(assets) != 1 || assets[0].Selected || assets[0].SourceEditionID != "OL456M" || assets[0].SourceKind != "other-edition" {
		t.Fatal(assets, e)
	}
	after, _ := s.Store.Book(ctx, id)
	if owned.ID != after.ID || owned.ISBN != after.ISBN || owned.Count != after.Count || owned.Revision != after.Revision {
		t.Fatal("preview changed owned metadata")
	}
	if e = s.Store.SelectCover(ctx, id, assets[0].ID); e != nil {
		t.Fatal(e)
	}
	after, _ = s.Store.Book(ctx, id)
	if owned.ISBN != after.ISBN || owned.Count != after.Count {
		t.Fatal("artwork selection changed edition or inventory")
	}
	if _, e = s.PreviewEdition(ctx, id, "OL999M"); e == nil || !strings.Contains(e.Error(), "linked work") {
		t.Fatal("unrelated edition accepted", e)
	}
	if _, e = s.Editions(ctx, id, 101); e == nil {
		t.Fatal("unbounded pagination accepted")
	}
}
