package httpapi

import (
	"context"
	"home-library/internal/store"
	"net/http"
)

func (s *Server) registerSubtitles(m *http.ServeMux) {
	m.HandleFunc("POST /api/subtitles/backfill", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Updates []store.SubtitleUpdate `json:"updates"`
		}
		if e := decode(w, r, &in); e != nil {
			failure(w, e)
			return
		}
		out, e := s.Store.FillSubtitles(r.Context(), in.Updates)
		if e != nil {
			failure(w, e)
			return
		}
		jsonResponse(w, 200, out)
	})
	m.HandleFunc("POST /api/subtitles/publish", func(w http.ResponseWriter, r *http.Request) {
		var in struct{}
		if e := decode(w, r, &in); e != nil {
			failure(w, e)
			return
		}
		if s.Pages != nil {
			if e := s.Pages.Ensure(context.WithoutCancel(r.Context())); e != nil {
				jsonResponse(w, 202, map[string]any{"saved": true, "page_warning": "Subtitles are saved. Retry publication when ready."})
				return
			}
		}
		jsonResponse(w, 200, map[string]any{"saved": true, "published": true})
	})
}
