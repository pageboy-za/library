package httpapi

import (
	"fmt"
	"home-library/internal/store"
	"net/http"
	"path"
	"strconv"
	"strings"
)

// An explicit public projection prevents new private fields leaking by default.
type memberBook struct {
	ID                 int64    `json:"id"`
	ISBN               string   `json:"isbn"`
	Title              string   `json:"title"`
	Subtitle           string   `json:"subtitle"`
	Authors            []string `json:"authors"`
	Publisher          string   `json:"publisher"`
	PublicationDate    string   `json:"publication_date"`
	SourceURL          string   `json:"source_url"`
	CoverURL           string   `json:"cover_url"`
	CoverThumbnailURL  string   `json:"cover_thumbnail_url"`
	Subjects           []string `json:"subjects"`
	FirstPublishedYear *int     `json:"first_published_year"`
	Description        string   `json:"description,omitempty"`
	Count              int      `json:"count"`
	Available          int      `json:"available"`
}

func publicBook(b store.Book) memberBook {
	v := memberBook{ID: b.ID, ISBN: b.ISBN, Title: b.Title, Subtitle: b.Subtitle, Authors: b.Authors, Publisher: b.Publisher, PublicationDate: b.PublicationDate, SourceURL: b.SourceURL, CoverURL: b.CoverURL, Subjects: b.Subjects, FirstPublishedYear: b.FirstPublishedYear, Count: b.Count, Available: b.Available}
	if b.CoverThumbnailURL != "" {
		v.CoverThumbnailURL = fmt.Sprintf("/api/member/books/%d/cover", b.ID)
	}
	return v
}
func (s *Server) publicCover(r *http.Request) bool {
	if unsafeMethod(r.Method) {
		return false
	}
	relative := strings.TrimPrefix(path.Clean(r.URL.Path), "/covers/")
	var n int
	e := s.Store.DB.QueryRowContext(r.Context(), `SELECT count(*) FROM books b JOIN cover_assets a ON a.id=b.selected_cover_asset_id AND a.book_id=b.id WHERE b.deleted_at IS NULL AND a.local_path=?`, relative).Scan(&n)
	return e == nil && n > 0
}
func (s *Server) registerMember(m *http.ServeMux) {
	m.HandleFunc("GET /api/member/books", func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page < 1 {
			page = 1
		}
		books, total, e := s.Store.ListWithOptions(r.Context(), r.URL.Query().Get("q"), 0, r.URL.Query().Get("availability"), page, store.ListOptions{Subject: r.URL.Query().Get("subject"), Sort: r.URL.Query().Get("sort")})
		if e != nil {
			failure(w, e)
			return
		}
		out := []memberBook{}
		for _, b := range books {
			out = append(out, publicBook(b))
		}
		jsonResponse(w, 200, map[string]any{"books": out, "total": total, "page": page})
	})
	m.HandleFunc("GET /api/member/books/{id}", func(w http.ResponseWriter, r *http.Request) {
		b, e := s.Store.Book(r.Context(), id(r))
		if e != nil {
			failure(w, e)
			return
		}
		d, e := s.Store.Discovery(r.Context(), id(r))
		if e != nil {
			failure(w, e)
			return
		}
		v := publicBook(b)
		v.Description = d.Description
		jsonResponse(w, 200, v)
	})
	m.HandleFunc("GET /api/member/books/{id}/cover", func(w http.ResponseWriter, r *http.Request) {
		if s.Covers == nil {
			http.NotFound(w, r)
			return
		}
		if _, e := s.Store.Book(r.Context(), id(r)); e != nil {
			http.NotFound(w, r)
			return
		}
		var asset int64
		if e := s.Store.DB.QueryRowContext(r.Context(), "SELECT selected_cover_asset_id FROM books WHERE id=?", id(r)).Scan(&asset); e != nil {
			http.NotFound(w, r)
			return
		}
		v, e := s.Covers.Thumbnail(r.Context(), id(r), asset)
		if e != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("ETag", v.ETag)
		if r.Header.Get("If-None-Match") == v.ETag {
			w.WriteHeader(304)
			return
		}
		w.Write(v.Data)
	})
}
