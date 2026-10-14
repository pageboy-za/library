package httpapi

import (
	"home-library/internal/store"
	"net/http"
)

func (s *Server) registerPreferences(m *http.ServeMux) {
	m.HandleFunc("GET /api/preferences", func(w http.ResponseWriter, r *http.Request) {
		p, e := s.Store.Preferences(r.Context())
		if e != nil {
			failure(w, e)
			return
		}
		jsonResponse(w, 200, p)
	})
	m.HandleFunc("PATCH /api/preferences", func(w http.ResponseWriter, r *http.Request) {
		var p store.Preferences
		if e := decode(w, r, &p); e != nil {
			failure(w, e)
			return
		}
		result, e := s.Store.SavePreferences(r.Context(), p.DefaultShelfID, p.Revision)
		if e != nil {
			failure(w, e)
			return
		}
		jsonResponse(w, 200, result)
	})
}
