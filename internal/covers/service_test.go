package covers

import (
	"bytes"
	"context"
	"fmt"
	"home-library/internal/store"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func testImage(c color.Color) []byte {
	img := image.NewRGBA(image.Rect(0, 0, 4, 6))
	for y := 0; y < 6; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}
func setup(t *testing.T) (*Service, int64) {
	t.Helper()
	s, e := store.Open(filepath.Join(t.TempDir(), "library.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.DB.Close() })
	b, e := s.Save(context.Background(), store.Input{Title: "Example", ISBN: "9780140328721", SourceID: "OL123M", Authors: []string{}, RequestKey: "first-token"})
	if e != nil {
		t.Fatal(e)
	}
	return New(s, t.TempDir()), b.ID
}

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestValidateImages(t *testing.T) {
	if _, e := validate(testImage(color.Black)); e != nil {
		t.Fatal(e)
	}
	for _, data := range [][]byte{[]byte(`<svg/>`), []byte(`not an image`), testImage(color.Black)[:20], make([]byte, MaxImageBytes+1)} {
		if _, e := validate(data); e == nil {
			t.Fatal("invalid image accepted")
		}
	}
	var b bytes.Buffer
	png.Encode(&b, image.NewGray(image.Rect(0, 0, 6001, 2)))
	if _, e := validate(b.Bytes()); e == nil {
		t.Fatal("oversized dimensions accepted")
	}
}
func TestUploadDedupAndSelectionIsolation(t *testing.T) {
	s, id := setup(t)
	ctx := context.Background()
	a, e := s.Upload(ctx, id, testImage(color.Black))
	if e != nil {
		t.Fatal(e)
	}
	again, e := s.Upload(ctx, id, testImage(color.Black))
	if e != nil || again.ID != a.ID {
		t.Fatalf("dedup %v", e)
	}
	b, e := s.Store.Save(ctx, store.Input{Title: "Second", Authors: []string{}, RequestKey: "second-token"})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Store.SelectCover(ctx, b.ID, a.ID); e != store.ErrNotFound {
		t.Fatalf("foreign asset selected %v", e)
	}
	selected, e := s.Store.SelectedCoverURL(ctx, id)
	if e != nil || selected != a.URL {
		t.Fatal(selected, e)
	}
	assets, e := s.Store.Covers(ctx, id)
	if e != nil || len(assets) != 1 || !assets[0].Selected {
		t.Fatal(assets, e)
	}
}
func TestFetchExactEditionDoesNotReplaceUpload(t *testing.T) {
	s, id := setup(t)
	ctx := context.Background()
	uploaded, e := s.Upload(ctx, id, testImage(color.Black))
	if e != nil {
		t.Fatal(e)
	}
	calls := 0
	s.HTTP = &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		s.last = time.Now().Add(-time.Second)
		body := []byte(`{"key":"/books/OL123M","covers":[123,456]}`)
		if r.URL.Host == "covers.openlibrary.org" {
			body = testImage(color.White)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(body)), Header: http.Header{}}, nil
	})}
	assets, e := s.Fetch(ctx, id)
	if e != nil {
		t.Fatal(e)
	}
	if len(assets) != 2 {
		t.Fatal("expected deduplicated uploaded + remote assets", assets)
	}
	selected, _ := s.Store.SelectedCoverURL(ctx, id)
	if selected != uploaded.URL {
		t.Fatal("fetch replaced upload")
	}
	if calls != 3 {
		t.Fatal(calls)
	}
	s.Fetch(ctx, id)
	if calls != 5 {
		t.Fatal("metadata and equal-byte ID recached; duplicate ID not retained", calls)
	}
}
func TestAllowedSources(t *testing.T) {
	for _, address := range []string{"http://openlibrary.org/a", "https://evil.test/a", "https://openlibrary.org:443/a", "https://user@openlibrary.org/a", "https://openlibrary.org.evil.test/a"} {
		u, _ := url.Parse(address)
		if allowed(u) == nil {
			t.Fatal(address)
		}
	}
	for _, address := range []string{"https://archive.org/download/l_covers_0012/l_covers_0012_58.zip/0012584244-L.jpg", "https://archive.org/download/olcovers90/olcovers90-L.zip/908302-L.jpg", "https://ia600100.us.archive.org/view_archive.php?archive=/5/items/l_covers_0012/l_covers_0012_58.zip&file=0012584244-L.jpg"} {
		u, _ := url.Parse(address)
		if e := allowed(u); e != nil {
			t.Fatal(address, e)
		}
	}
	for _, address := range []string{"https://archive.org/download/arbitrary/file.zip/123-L.jpg", "https://archive.org/metadata/l_covers_0012", "https://ia600100.us.archive.org/view_archive.php?archive=/5/items/arbitrary/file.zip&file=123-L.jpg", "https://ia600100.us.archive.org/view_archive.php?archive=/5/items/l_covers_0012/file.zip&file=../../secret", "https://ia600100.us.archive.org/download/arbitrary", "https://ia600100.us.archive.org.evil.test/view_archive.php?archive=/5/items/l_covers_0012/file.zip&file=123-L.jpg"} {
		u, _ := url.Parse(address)
		if allowed(u) == nil {
			t.Fatal("unsafe archive URL allowed", address)
		}
	}
}
func TestEdition404UsesISBNFallback(t *testing.T) {
	s, id := setup(t)
	s.HTTP = &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		s.last = time.Now().Add(-time.Second)
		if r.URL.Host == "openlibrary.org" {
			return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader("missing")), Header: http.Header{}}, nil
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(testImage(color.White))), Header: http.Header{}}, nil
	})}
	assets, e := s.Fetch(context.Background(), id)
	if e != nil || len(assets) != 1 || assets[0].SourceKind != "openlibrary-isbn" {
		t.Fatal(assets, e)
	}
}
func TestAssetServingBlocksDirectoryAndMultipartLimit(t *testing.T) {
	s, id := setup(t)
	a, e := s.Upload(context.Background(), id, testImage(color.Black))
	if e != nil {
		t.Fatal(e)
	}
	mux := http.NewServeMux()
	s.Register(mux, nil)
	for _, path := range []string{"/covers/", "/covers/.cover-secret", "/covers/arbitrary.html"} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 404 {
			t.Fatal(path, w.Code)
		}
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", a.URL, nil))
	if w.Code != 200 || !strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
		t.Fatal(w.Code, w.Header())
	}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/books/1/covers/upload", strings.NewReader("invalid")))
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
}

func TestFindCoversWaitsForExplicitSelection(t *testing.T) {
	for _, initial := range []string{"none", "saved", "isbn-fallback"} {
		t.Run(initial, func(t *testing.T) {
			s, id := setup(t)
			ctx := context.Background()
			if initial == "saved" {
				if _, err := s.Upload(ctx, id, testImage(color.Black)); err != nil {
					t.Fatal(err)
				}
			}
			before, err := s.Store.Book(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			s.HTTP = &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				s.last = time.Now().Add(-time.Second)
				body := []byte(`{"key":"/books/OL123M","covers":[123,456]}`)
				if initial == "isbn-fallback" {
					body = []byte(`{"key":"/books/OL123M","covers":[]}`)
				}
				if r.URL.Host == "covers.openlibrary.org" {
					body = testImage(color.White)
					if strings.Contains(r.URL.Path, "456") {
						body = testImage(color.RGBA{R: 255, A: 255})
					}
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(body)), Header: http.Header{}}, nil
			})}
			published := 0
			mux := http.NewServeMux()
			s.Register(mux, func(context.Context) error { published++; return nil })
			for i := 0; i < 2; i++ {
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/books/1/covers/fetch", strings.NewReader(`{}`)))
				if w.Code != 200 {
					t.Fatal(w.Code, w.Body.String())
				}
			}
			after, err := s.Store.Book(ctx, id)
			if err != nil || !reflect.DeepEqual(before, after) || published != 0 {
				t.Fatal("finding covers changed the book or published pages", before, after, published, err)
			}
			assets, err := s.Store.Covers(ctx, id)
			want := 2
			if initial == "saved" {
				want = 3
			}
			if initial == "isbn-fallback" {
				want = 1
			}
			if err != nil || len(assets) != want {
				t.Fatal("missing cover choices", assets, err)
			}
			chosen := assets[len(assets)-1]
			if chosen.Selected {
				t.Fatal("alternative was selected automatically")
			}
			w := httptest.NewRecorder()
			r := httptest.NewRequest("POST", "/api/books/1/covers/select", strings.NewReader(fmt.Sprintf(`{"asset_id":%d}`, chosen.ID)))
			r.Header.Set("Content-Type", "application/json")
			mux.ServeHTTP(w, r)
			selected, err := s.Store.SelectedCoverURL(ctx, id)
			if w.Code != 200 || err != nil || selected != chosen.URL || published != 1 {
				t.Fatal("explicit choice did not save and publish", w.Code, selected, published, err)
			}
		})
	}
}
