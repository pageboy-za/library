package httpapi

import (
	"errors"
	"home-library/internal/store"
	"net/http"
	"strings"
)

func (s *Server) registerMemberLending(m *http.ServeMux) {
	m.HandleFunc("POST /api/member/loans", func(w http.ResponseWriter, r *http.Request) {
		seed, ok := s.memberCSRF(w, r)
		if !ok {
			return
		}
		if !s.memberLendingLimiter.allowWithin(seed, 60, 120) {
			w.Header().Set("Retry-After", "600")
			jsonResponse(w, 429, map[string]string{"error": "Please wait before scanning again, or ask a librarian for help"})
			return
		}
		var in struct {
			BookID   int64  `json:"book_id"`
			CardCode string `json:"card_code"`
			Key      string `json:"request_key"`
		}
		if err := decode(w, r, &in); err != nil {
			failure(w, err)
			return
		}
		if len(in.Key) < 8 || len(in.Key) > 128 {
			jsonResponse(w, 400, map[string]string{"error": "Refresh this book before lending"})
			return
		}
		_, err := s.Store.MemberCheckout(r.Context(), in.BookID, strings.TrimSpace(in.CardCode), in.Key)
		switch {
		case errors.Is(err, store.ErrMemberPass):
			jsonResponse(w, 403, map[string]string{"error": err.Error()})
		case errors.Is(err, store.ErrNoLendableCopy), errors.Is(err, store.ErrConflict):
			jsonResponse(w, 409, map[string]string{"error": "This book's availability has changed. Refresh it before lending again"})
		case err != nil:
			jsonResponse(w, 500, map[string]string{"error": "The loan could not be confirmed. Retry safely to check it"})
		default:
			// No borrower identity, history or internal loan identifiers on this route.
			jsonResponse(w, 200, map[string]bool{"borrowed": true})
		}
	})
}
