package covers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"home-library/internal/store"
	"image"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestDerivedThumbnailDimensionsCacheAndRestore(t *testing.T) {
	for _, provider := range []bool{true, false} {
		t.Run(strconv.FormatBool(provider), func(t *testing.T) {
			s, id := setup(t)
			ctx := context.Background()
			data := cameraJPEG(t, 6, 400, 800)
			var asset store.CoverAsset
			var e error
			if provider {
				asset, e = s.save(ctx, store.CoverAsset{BookID: id, SourceKind: "openlibrary", SourceEditionID: "OL123M"}, data, true, false)
			} else {
				asset, e = s.Upload(ctx, id, data)
			}
			if e != nil {
				t.Fatal(e)
			}
			original, _ := os.ReadFile(filepath.Join(s.Directory, asset.LocalPath))
			before, _ := s.Store.Book(ctx, id)
			assets, e := s.Assets(ctx, id)
			if e != nil || len(assets) != 1 || assets[0].Width != 800 || assets[0].Height != 400 || assets[0].ThumbnailWidth != 320 || assets[0].ThumbnailHeight != 160 {
				t.Fatal(assets, e)
			}
			selectedURL, e := s.Store.SelectedCoverThumbnailURL(ctx, id)
			if e != nil || selectedURL != assets[0].ThumbnailURL {
				t.Fatal(selectedURL, e)
			}
			thumb, e := s.Thumbnail(ctx, id, asset.ID)
			if e != nil {
				t.Fatal(e)
			}
			cfg, format, e := image.DecodeConfig(bytes.NewReader(thumb.Data))
			if e != nil || format != "jpeg" || cfg.Width != 320 || cfg.Height != 160 || bytes.Contains(thumb.Data, []byte("Exif")) {
				t.Fatal(cfg, format, e)
			}
			sum := sha256.Sum256(thumb.Data)
			if hex.EncodeToString(sum[:]) != thumb.ETag {
				t.Fatal("wrong ETag")
			}
			cachedPath := filepath.Join(s.Directory, "derived", asset.Checksum+"-320-v1.jpg")
			info, _ := os.Stat(cachedPath)
			again, e := s.Thumbnail(ctx, id, asset.ID)
			nextInfo, _ := os.Stat(cachedPath)
			if e != nil || !bytes.Equal(again.Data, thumb.Data) || !info.ModTime().Equal(nextInfo.ModTime()) {
				t.Fatal("immutable cache rewritten", e)
			}
			os.WriteFile(cachedPath, []byte("corrupt derivative"), 0600)
			repaired, e := s.Thumbnail(ctx, id, asset.ID)
			if e != nil || !bytes.Equal(repaired.Data, thumb.Data) {
				t.Fatal("derived corruption not rebuilt", e)
			}
			os.RemoveAll(filepath.Join(s.Directory, "derived"))
			restored, e := s.Thumbnail(ctx, id, asset.ID)
			if e != nil || !bytes.Equal(restored.Data, thumb.Data) {
				t.Fatal("missing derived variant not rebuilt", e)
			}
			unchanged, _ := os.ReadFile(filepath.Join(s.Directory, asset.LocalPath))
			after, _ := s.Store.Book(ctx, id)
			if !bytes.Equal(original, unchanged) || before.Revision != after.Revision || before.Count != after.Count || before.ISBN != after.ISBN || before.CoverURL != after.CoverURL {
				t.Fatal("thumbnail mutated authoritative data")
			}
			os.Remove(filepath.Join(s.Directory, asset.LocalPath))
			if _, e = s.Thumbnail(ctx, id, asset.ID); e == nil {
				t.Fatal("cached thumbnail served despite missing original")
			}
		})
	}
}
func TestThumbnailNoUpscaleAndConditionalHTTP(t *testing.T) {
	s, id := setup(t)
	ctx := context.Background()
	asset, e := s.Upload(ctx, id, cameraJPEG(t, 1, 4, 8))
	if e != nil {
		t.Fatal(e)
	}
	v, e := s.Thumbnail(ctx, id, asset.ID)
	if e != nil || v.Width != 4 || v.Height != 8 {
		t.Fatal(v, e)
	}
	mux := http.NewServeMux()
	s.Register(mux, nil)
	url := "/api/books/" + strconv.FormatInt(id, 10) + "/covers/" + strconv.FormatInt(asset.ID, 10) + "/thumbnail?v=1"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", url, nil))
	if w.Code != 200 || !strings.Contains(w.Header().Get("Cache-Control"), "immutable") || w.Header().Get("ETag") == "" || w.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatal(w.Code, w.Header())
	}
	req := httptest.NewRequest("GET", url, nil)
	req.Header.Set("If-None-Match", w.Header().Get("ETag"))
	cached := httptest.NewRecorder()
	mux.ServeHTTP(cached, req)
	if cached.Code != 304 || cached.Body.Len() != 0 {
		t.Fatal(cached.Code, cached.Body.String())
	}
	bad := httptest.NewRecorder()
	mux.ServeHTTP(bad, httptest.NewRequest("GET", "/api/books/999/covers/1/thumbnail", nil))
	if bad.Code != 404 {
		t.Fatal("thumbnail ownership not enforced", bad.Code)
	}
}
