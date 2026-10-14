package httpapi

import (
	"errors"
	"net/http"
)

func (s *Server) registerDeletion(m *http.ServeMux) {
	m.HandleFunc("GET /api/deleted-books", func(w http.ResponseWriter, r *http.Request) {
		books, e := s.Store.DeletedBooks(r.Context())
		if e != nil {
			failure(w, e)
			return
		}
		jsonResponse(w, 200, books)
	})
	for _, operation := range []string{"delete", "restore"} {
		deleted := operation == "delete"
		m.HandleFunc("POST /api/books/{id}/"+operation, func(w http.ResponseWriter, r *http.Request) {
			var in struct {
				ExpectedCopyCount *int   `json:"expected_copy_count"`
				Revision          int    `json:"revision"`
				RequestKey        string `json:"request_key"`
			}
			if e := decode(w, r, &in); e != nil {
				failure(w, e)
				return
			}
			if in.ExpectedCopyCount == nil || *in.ExpectedCopyCount < 0 {
				failure(w, errors.New("Reload the book to confirm its current copy count"))
				return
			}
			b, e := s.Store.SetBookDeleted(r.Context(), id(r), in.Revision, deleted, in.RequestKey, *in.ExpectedCopyCount)
			if e != nil {
				failure(w, e)
				return
			}
			// Database visibility is immediate, including when static generation fails.
			out := map[string]any{"book": b}
			if s.Pages != nil {
				if e = s.Pages.Ensure(r.Context()); e != nil {
					out["page_warning"] = "Your change is saved. Book pages will be rebuilt on the next visit."
				}
			}
			jsonResponse(w, 200, out)
		})
	}
}
