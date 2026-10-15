package httpapi

import (
	"encoding/json"
	"errors"
	"home-library/internal/store"
	"net/http"
)

func (s *Server) registerBibliography(m *http.ServeMux) {
	m.HandleFunc("POST /api/metadata/backfill", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Updates []store.MetadataBackfillUpdate `json:"updates"`
		}
		if err := decode(w, r, &in); err != nil {
			failure(w, err)
			return
		}
		out, err := s.Store.FillMetadata(r.Context(), in.Updates)
		if err != nil {
			failure(w, err)
			return
		}
		if out.Changed > 0 && (s.Pages != nil || s.Covers != nil) {
			s.queuePublication(0)
		}
		jsonResponse(w, 200, out)
	})
	m.HandleFunc("GET /api/book-subjects", func(w http.ResponseWriter, r *http.Request) {
		v, err := s.Store.BookSubjects(r.Context())
		if err != nil {
			failure(w, err)
			return
		}
		jsonResponse(w, 200, map[string]any{"subjects": v})
	})
	m.HandleFunc("PATCH /api/books/{id}/bibliography", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			FirstPublishedYear json.RawMessage `json:"first_published_year"`
			Revision           int             `json:"revision"`
		}
		if err := decode(w, r, &in); err != nil {
			failure(w, err)
			return
		}
		if len(in.FirstPublishedYear) == 0 {
			failure(w, errors.New("Include a first publication year or null to clear it"))
			return
		}
		var year *int
		if err := json.Unmarshal(in.FirstPublishedYear, &year); err != nil {
			failure(w, errors.New("Use a whole year or null"))
			return
		}
		v, err := s.Store.SaveBibliography(r.Context(), id(r), store.Bibliography{FirstPublishedYear: year, Revision: in.Revision})
		if err != nil {
			failure(w, err)
			return
		}
		if s.Pages != nil || s.Covers != nil {
			s.queuePublication(id(r))
		}
		jsonResponse(w, 200, v)
	})
}
