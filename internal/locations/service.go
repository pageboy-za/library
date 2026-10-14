// Package locations exposes stable location management behind the app boundary.
package locations

import (
	"encoding/json"
	"home-library/internal/store"
	"io"
	"mime"
	"net/http"
	"strconv"
)

type Service struct{ Store *store.Store }

func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	media, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || media != "application/json" {
		reply(w, 415, map[string]string{"error": "Use application/json"})
		return false
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil {
		reply(w, 400, map[string]string{"error": "Invalid request"})
		return false
	}
	if d.Decode(&struct{}{}) != io.EOF {
		reply(w, 400, map[string]string{"error": "Expected one JSON object"})
		return false
	}
	return true
}
func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/bookcases", func(w http.ResponseWriter, r *http.Request) {
		out, e := s.Store.Bookcases(r.Context())
		if e != nil {
			reply(w, 500, map[string]string{"error": "Could not load bookcases"})
			return
		}
		reply(w, 200, out)
	})
	for _, kind := range []string{"shelves", "bookcases"} {
		mux.HandleFunc("PATCH /api/"+kind+"/{id}", func(w http.ResponseWriter, r *http.Request) { s.mutate(w, r, kind, false) })
		mux.HandleFunc("POST /api/"+kind+"/{id}/remove", func(w http.ResponseWriter, r *http.Request) { s.mutate(w, r, kind, true) })
	}
}
func (s *Service) mutate(w http.ResponseWriter, r *http.Request, kind string, remove bool) {
	id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if e != nil || id <= 0 {
		reply(w, 400, map[string]string{"error": "Invalid location ID"})
		return
	}
	var in struct {
		Name     string `json:"name"`
		Expected string `json:"expected_name"`
		Key      string `json:"request_key"`
		Revision int    `json:"expected_revision"`
	}
	if !decode(w, r, &in) {
		return
	}
	if kind == "shelves" {
		if remove {
			e = s.Store.RemoveShelf(r.Context(), id, in.Expected, in.Revision, in.Key)
		} else {
			e = s.Store.RenameShelf(r.Context(), id, in.Name, in.Expected, in.Revision, in.Key)
		}
	} else {
		if remove {
			e = s.Store.RemoveBookcase(r.Context(), id, in.Expected, in.Revision, in.Key)
		} else {
			e = s.Store.RenameBookcase(r.Context(), id, in.Name, in.Expected, in.Revision, in.Key)
		}
	}
	if e != nil {
		reply(w, 409, map[string]string{"error": e.Error()})
		return
	}
	reply(w, 200, map[string]bool{"saved": true})
}
