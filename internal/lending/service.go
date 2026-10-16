// Package lending provides shared lending operations and an HTTP adapter.
package lending

import (
	"context"
	"encoding/json"
	"home-library/internal/store"
	"io"
	"mime"
	"net/http"
	"strconv"
	"time"
)

type Service struct{ Store *store.Store }

func (s *Service) Checkout(ctx context.Context, copyID, borrowerID int64, due, key string) (int64, error) {
	return s.Store.Checkout(ctx, copyID, borrowerID, due, key)
}
func (s *Service) Return(ctx context.Context, id int64, key string) (int64, error) {
	return s.Store.ReturnLoan(ctx, id, key)
}
func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func failure(w http.ResponseWriter, e error) { reply(w, 409, map[string]string{"error": e.Error()}) }
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		reply(w, 415, map[string]string{"error": "Use application/json"})
		return false
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
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
	mux.HandleFunc("POST /api/copies/{id}/archive", func(w http.ResponseWriter, r *http.Request) {
		id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if e != nil || id <= 0 {
			reply(w, 400, map[string]string{"error": "Invalid copy ID"})
			return
		}
		var in struct {
			Revision int    `json:"revision"`
			Key      string `json:"request_key"`
		}
		if !decode(w, r, &in) {
			return
		}
		id, e = s.Store.ArchiveCopy(r.Context(), id, in.Revision, in.Key)
		if e != nil {
			failure(w, e)
			return
		}
		reply(w, 200, map[string]int64{"id": id})
	})
	mux.HandleFunc("PATCH /api/borrowers/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if e != nil || id <= 0 {
			reply(w, 400, map[string]string{"error": "Invalid borrower ID"})
			return
		}
		var in struct {
			Name          string `json:"name"`
			Notes         string `json:"notes"`
			ExpectedName  string `json:"expected_name"`
			ExpectedNotes string `json:"expected_notes"`
		}
		if !decode(w, r, &in) {
			return
		}
		if e = s.Store.EditBorrower(r.Context(), id, in.Name, in.Notes, in.ExpectedName, in.ExpectedNotes); e != nil {
			failure(w, e)
			return
		}
		reply(w, 200, map[string]bool{"saved": true})
	})
	mux.HandleFunc("GET /api/loans", func(w http.ResponseWriter, r *http.Request) {
		borrower := int64(0)
		var e error
		if raw := r.URL.Query().Get("borrower_id"); raw != "" {
			borrower, e = strconv.ParseInt(raw, 10, 64)
			if e != nil || borrower < 0 {
				reply(w, 400, map[string]string{"error": "Invalid borrower filter"})
				return
			}
		}
		page := 1
		if raw := r.URL.Query().Get("page"); raw != "" {
			page, e = strconv.Atoi(raw)
			if e != nil || page < 1 || page > 100000 {
				reply(w, 400, map[string]string{"error": "Invalid page"})
				return
			}
		}
		out, e := s.Store.ActiveLoans(r.Context(), borrower, r.URL.Query().Get("due"), page, time.Now().UTC().Format("2006-01-02"))
		if e != nil {
			failure(w, e)
			return
		}
		reply(w, 200, out)
	})
	mux.HandleFunc("GET /api/borrowers", func(w http.ResponseWriter, r *http.Request) {
		out, e := s.Store.Borrowers(r.Context())
		if e != nil {
			failure(w, e)
			return
		}
		reply(w, 200, out)
	})
	mux.HandleFunc("POST /api/borrowers", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Name  string `json:"name"`
			Notes string `json:"notes"`
			Key   string `json:"request_key"`
		}
		if !decode(w, r, &in) {
			return
		}
		id, e := s.Store.AddBorrower(r.Context(), in.Name, in.Notes, in.Key)
		if e != nil {
			failure(w, e)
			return
		}
		reply(w, 201, map[string]int64{"id": id})
	})
	mux.HandleFunc("POST /api/borrowers/{id}/archive", func(w http.ResponseWriter, r *http.Request) {
		var in struct{}
		if !decode(w, r, &in) {
			return
		}
		id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if e != nil || id <= 0 {
			reply(w, 400, map[string]string{"error": "Invalid borrower ID"})
			return
		}
		if e = s.Store.ArchiveBorrower(r.Context(), id); e != nil {
			failure(w, e)
			return
		}
		reply(w, 200, map[string]bool{"archived": true})
	})
	mux.HandleFunc("GET /api/books/{id}/loans", func(w http.ResponseWriter, r *http.Request) {
		id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if e != nil || id <= 0 {
			reply(w, 400, map[string]string{"error": "Invalid book ID"})
			return
		}
		out, e := s.Store.Loans(r.Context(), id)
		if e != nil {
			failure(w, e)
			return
		}
		reply(w, 200, out)
	})
	mux.HandleFunc("POST /api/loans", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			CopyID     int64  `json:"copy_id"`
			BorrowerID int64  `json:"borrower_id"`
			CardCode   string `json:"card_code"`
			Due        string `json:"due_date"`
			Key        string `json:"request_key"`
		}
		if !decode(w, r, &in) {
			return
		}
		id, e := s.Store.CheckoutWithCard(r.Context(), in.CopyID, in.BorrowerID, in.Due, in.Key, in.CardCode)
		if e != nil {
			failure(w, e)
			return
		}
		reply(w, 201, map[string]int64{"id": id})
	})
	mux.HandleFunc("POST /api/loans/{id}/return", func(w http.ResponseWriter, r *http.Request) {
		id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if e != nil || id <= 0 {
			reply(w, 400, map[string]string{"error": "Invalid loan ID"})
			return
		}
		var in struct {
			Key string `json:"request_key"`
		}
		if !decode(w, r, &in) {
			return
		}
		id, e = s.Return(r.Context(), id, in.Key)
		if e != nil {
			failure(w, e)
			return
		}
		reply(w, 200, map[string]int64{"id": id})
	})
}
