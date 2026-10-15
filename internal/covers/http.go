package covers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"home-library/internal/store"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

// Register must be inside the application's host/origin security middleware.
// changed republishes static metadata after a selection; persistence succeeds
// independently if publishing is temporarily unavailable.
func (s *Service) Register(m *http.ServeMux, changed func(context.Context) error) {
	respond := func(w http.ResponseWriter, status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(v)
	}
	fail := func(w http.ResponseWriter, e error) {
		status := 400
		if errors.Is(e, store.ErrNotFound) {
			status = 404
		}
		respond(w, status, map[string]string{"error": e.Error()})
	}
	id := func(r *http.Request) int64 { v, _ := strconv.ParseInt(r.PathValue("id"), 10, 64); return v }
	result := func(w http.ResponseWriter, r *http.Request) {
		assets, e := s.Assets(r.Context(), id(r))
		if e != nil {
			fail(w, e)
			return
		}
		warning := ""
		if changed != nil {
			if e = changed(context.WithoutCancel(r.Context())); e != nil {
				warning = "Cover saved. The book page will refresh when generation is available. You can keep using the library."
			}
		}
		respond(w, 200, map[string]any{"assets": assets, "page_warning": warning})
	}
	m.HandleFunc("GET /api/books/{id}/covers", func(w http.ResponseWriter, r *http.Request) {
		b, e := s.Store.Book(r.Context(), id(r))
		if e != nil {
			fail(w, e)
			return
		}
		a, e := s.Assets(r.Context(), id(r))
		if e != nil {
			fail(w, e)
			return
		}
		editionURL := ""
		if editionID.MatchString(b.SourceID) {
			editionURL = "https://openlibrary.org/books/" + b.SourceID
		}
		respond(w, 200, map[string]any{"assets": a, "edition_url": editionURL, "owned_edition": map[string]string{"title": b.Title, "isbn": b.ISBN, "publisher": b.Publisher, "publication_date": b.PublicationDate, "edition_id": b.SourceID}})
	})
	m.HandleFunc("GET /api/books/{id}/covers/{asset_id}/thumbnail", func(w http.ResponseWriter, r *http.Request) {
		assetID, _ := strconv.ParseInt(r.PathValue("asset_id"), 10, 64)
		v, e := s.Thumbnail(r.Context(), id(r), assetID)
		if e != nil {
			fail(w, e)
			return
		}
		etag := `"` + v.ETag + `"`
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(v.Data)))
		w.Write(v.Data)
	})
	m.HandleFunc("GET /api/books/{id}/covers/{asset_id}/download", func(w http.ResponseWriter, r *http.Request) {
		assetID, _ := strconv.ParseInt(r.PathValue("asset_id"), 10, 64)
		data, e := s.Download(r.Context(), id(r), assetID)
		if e != nil {
			fail(w, e)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="book-%d-cover.jpg"`, id(r)))
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.WriteHeader(200)
		w.Write(data)
	})
	m.HandleFunc("GET /api/books/{id}/covers/editions", func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page == 0 {
			page = 1
		}
		v, e := s.Editions(r.Context(), id(r), page)
		if e != nil {
			fail(w, e)
			return
		}
		respond(w, 200, v)
	})
	m.HandleFunc("POST /api/books/{id}/covers/edition", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			EditionID string `json:"edition_id"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		d := json.NewDecoder(r.Body)
		d.DisallowUnknownFields()
		if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" || d.Decode(&in) != nil || d.Decode(new(any)) != io.EOF {
			fail(w, errors.New("Invalid edition selection"))
			return
		}
		assets, e := s.PreviewEdition(r.Context(), id(r), in.EditionID)
		if e != nil {
			fail(w, e)
			return
		}
		assets, e = s.Assets(r.Context(), id(r))
		if e != nil {
			fail(w, e)
			return
		}
		respond(w, 200, map[string]any{"assets": assets})
	})
	m.HandleFunc("POST /api/books/{id}/covers/fetch", func(w http.ResponseWriter, r *http.Request) {
		if _, e := s.Find(r.Context(), id(r)); e != nil {
			fail(w, e)
			return
		}
		assets, e := s.Assets(r.Context(), id(r))
		if e != nil {
			fail(w, e)
			return
		}
		respond(w, 200, map[string]any{"assets": assets})
	})
	m.HandleFunc("POST /api/books/{id}/covers/select", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			AssetID int64 `json:"asset_id"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		d := json.NewDecoder(r.Body)
		d.DisallowUnknownFields()
		if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" || d.Decode(&in) != nil || d.Decode(new(any)) != io.EOF {
			fail(w, errors.New("Invalid cover selection"))
			return
		}
		if e := s.Store.SelectCover(r.Context(), id(r), in.AssetID); e != nil {
			fail(w, e)
			return
		}
		result(w, r)
	})
	m.HandleFunc("POST /api/books/{id}/covers/upload", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, MaxImageBytes+(64<<10))
		if e := r.ParseMultipartForm(MaxImageBytes + (64 << 10)); e != nil {
			fail(w, errors.New("Choose an image up to 8 MB"))
			return
		}
		if r.MultipartForm != nil {
			defer r.MultipartForm.RemoveAll()
		}
		f, _, e := r.FormFile("image")
		if e != nil {
			fail(w, errors.New("Choose a cover image"))
			return
		}
		defer f.Close()
		data, e := io.ReadAll(io.LimitReader(f, MaxImageBytes+1))
		if e != nil {
			fail(w, e)
			return
		}
		if _, e = s.Upload(r.Context(), id(r), data); e != nil {
			fail(w, e)
			return
		}
		result(w, r)
	})
	files := http.StripPrefix("/covers/", http.FileServer(http.Dir(s.Directory)))
	assetPath := regexp.MustCompile(`^/covers/[0-9a-f]{64}\.(jpg|png|webp)$`)
	m.HandleFunc("GET /covers/", func(w http.ResponseWriter, r *http.Request) {
		if !assetPath.MatchString(r.URL.Path) {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		files.ServeHTTP(w, r)
	})
}
