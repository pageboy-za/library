package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"home-library/internal/catalogue"
	"home-library/internal/covers"
	"home-library/internal/lending"
	"home-library/internal/library"
	"home-library/internal/locations"
	"home-library/internal/metadata"
	"home-library/internal/store"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

type Server struct {
	Version         string
	Commit          string
	MaintenancePath string
	ActiveWrites    atomic.Int64
	BackupDirectory string
	Store           *store.Store
	Metadata        *metadata.Client
	Web             string
	Covers          *covers.Service
	Enrichment      *catalogue.EnrichmentService
	Pages           interface {
		Ensure(context.Context) error
		Directory() string
	}
}

// Inventory remains authoritative if publishing fails. A book route retries
// publication and returns 503 rather than silently serving stale metadata.
func (s *Server) publish(w http.ResponseWriter, r *http.Request, b store.Book) {
	if s.Covers != nil && b.CoverURL == "" && b.ISBN != "" {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 30*time.Second)
		if _, e := s.Covers.Fetch(ctx, b.ID); e != nil {
			log.Printf("Cover lookup for book %d: %v", b.ID, e)
		}
		cancel()
		if fresh, e := s.Store.Book(r.Context(), b.ID); e == nil {
			b = fresh
		}
	}
	if s.Pages != nil {
		if e := s.Pages.Ensure(context.WithoutCancel(r.Context())); e != nil {
			log.Printf("Publish book %d: %v", b.ID, e)
			jsonResponse(w, 202, struct {
				store.Book
				Warning string `json:"page_warning"`
			}{b, "Book saved, but its page could not be updated. Retry saving or open the book to retry; no extra copy will be created."})
			return
		}
	}
	jsonResponse(w, 200, b)
}
func jsonResponse(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func failure(w http.ResponseWriter, e error) {
	status := 400
	if errors.Is(e, store.ErrConflict) {
		status = 409
	}
	if errors.Is(e, store.ErrNotFound) {
		status = 404
	}
	jsonResponse(w, status, map[string]string{"error": e.Error()})
}
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		return errors.New("Use application/json")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return errors.New("Invalid or oversized request")
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("Send one JSON object")
	}
	return nil
}
func id(r *http.Request) int64 { v, _ := strconv.ParseInt(r.PathValue("id"), 10, 64); return v }
func (s *Server) Handler() http.Handler {
	m := http.NewServeMux()
	if assets, ok := s.Pages.(interface{ AssetsDirectory() string }); ok {
		m.HandleFunc("GET /_astro/", func(w http.ResponseWriter, r *http.Request) {
			relative := strings.TrimPrefix(path.Clean(r.URL.Path), "/_astro/")
			file := filepath.Join(assets.AssetsDirectory(), filepath.FromSlash(relative))
			info, err := os.Stat(file)
			if err != nil || !info.Mode().IsRegular() {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			http.ServeFile(w, r, file)
		})
	}
	(&lending.Service{Store: s.Store}).Register(m)
	(&locations.Service{Store: s.Store}).Register(m)
	s.registerBackupStatus(m)
	enrichment := s.Enrichment
	if enrichment == nil {
		enrichment = catalogue.NewEnrichment(s.Store)
	}
	m.HandleFunc("POST /api/books/{id}/discovery/enrich", func(w http.ResponseWriter, r *http.Request) {
		var in struct{}
		if e := decode(w, r, &in); e != nil {
			failure(w, e)
			return
		}
		v, e := enrichment.Enrich(r.Context(), id(r))
		if e != nil {
			failure(w, e)
			return
		}
		if v.Changed && s.Pages != nil {
			if e = s.Pages.Ensure(context.WithoutCancel(r.Context())); e != nil {
				jsonResponse(w, 202, map[string]any{"discovery": v.Discovery, "source_url": v.SourceURL, "work_level": v.WorkLevel, "changed": v.Changed, "page_warning": "Details saved; static page generation failed. Open the book to retry."})
				return
			}
		}
		jsonResponse(w, 200, v)
	})
	if s.Covers != nil {
		var changed func(context.Context) error
		if s.Pages != nil {
			changed = s.Pages.Ensure
		}
		s.Covers.Register(m, changed)
	}
	m.HandleFunc("GET /api/discovery", func(w http.ResponseWriter, r *http.Request) {
		v, e := (catalogue.DiscoveryService{Store: s.Store}).Search(r.Context(), r.URL.Query().Get("q"))
		if e != nil {
			failure(w, e)
			return
		}
		jsonResponse(w, 200, v)
	})
	m.HandleFunc("GET /api/books/{id}/discovery", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Store.Discovery(r.Context(), id(r))
		if e != nil {
			failure(w, e)
			return
		}
		jsonResponse(w, 200, v)
	})
	m.HandleFunc("PATCH /api/books/{id}/discovery", func(w http.ResponseWriter, r *http.Request) {
		var in store.Discovery
		if e := decode(w, r, &in); e != nil {
			failure(w, e)
			return
		}
		v, e := s.Store.SaveDiscovery(r.Context(), id(r), in)
		if e != nil {
			failure(w, e)
			return
		}
		if s.Pages != nil {
			if e = s.Pages.Ensure(context.WithoutCancel(r.Context())); e != nil {
				jsonResponse(w, 202, map[string]any{"saved": v, "page_warning": "Themes saved, but page generation failed. Open the book to retry."})
				return
			}
		}
		jsonResponse(w, 200, v)
	})
	m.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		if e := s.Store.DB.PingContext(r.Context()); e != nil {
			jsonResponse(w, 503, map[string]string{"status": "unavailable"})
			return
		}
		jsonResponse(w, 200, map[string]string{"status": "ready"})
	})
	m.HandleFunc("GET /api/version", func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, 200, map[string]any{"version": s.Version, "commit": s.Commit, "active_writes": s.ActiveWrites.Load()})
	})
	m.HandleFunc("GET /api/books", func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page < 1 {
			page = 1
		}
		shelf, _ := strconv.ParseInt(r.URL.Query().Get("shelf_id"), 10, 64)
		b, total, e := s.Store.List(r.Context(), r.URL.Query().Get("q"), shelf, r.URL.Query().Get("availability"), page)
		if e != nil {
			failure(w, e)
			return
		}
		jsonResponse(w, 200, map[string]any{"books": b, "total": total, "page": page})
	})
	m.HandleFunc("GET /api/books/{id}", func(w http.ResponseWriter, r *http.Request) {
		b, e := s.Store.Book(r.Context(), id(r))
		if e != nil {
			failure(w, e)
			return
		}
		jsonResponse(w, 200, b)
	})
	m.HandleFunc("GET /api/isbn/{isbn}", func(w http.ResponseWriter, r *http.Request) {
		isbn, e := library.ISBN(r.PathValue("isbn"))
		if e != nil {
			failure(w, e)
			return
		}
		b, e := s.Store.ByISBN(r.Context(), isbn)
		if e == nil {
			jsonResponse(w, 200, map[string]any{"existing": b, "isbn": isbn})
			return
		}
		if !errors.Is(e, store.ErrNotFound) {
			failure(w, e)
			return
		}
		client := s.Metadata
		if client == nil {
			client = metadata.New()
		}
		result, e := client.LookupDetails(r.Context(), isbn)
		out := map[string]any{"isbn": isbn, "draft": result.Draft, "fetched_at": result.FetchedAt, "cached": result.Cached}
		if result.Warning != "" {
			out["warning"] = result.Warning
		}
		if e != nil {
			out["draft"] = store.Input{ISBN: isbn, Authors: []string{}}
			out["warning"] = e.Error()
		}
		jsonResponse(w, 200, out)
	})
	m.HandleFunc("POST /api/books/{id}/metadata/refresh", func(w http.ResponseWriter, r *http.Request) {
		var in struct{}
		if e := decode(w, r, &in); e != nil {
			failure(w, e)
			return
		}
		b, e := s.Store.Book(r.Context(), id(r))
		if e != nil {
			failure(w, e)
			return
		}
		if b.ISBN == "" {
			failure(w, errors.New("This book has no saved ISBN. Enter details manually"))
			return
		}
		client := s.Metadata
		if client == nil {
			client = metadata.New()
		}
		result, e := client.Refresh(r.Context(), b.ISBN)
		if e != nil {
			failure(w, e)
			return
		}
		// Lookup only prepares a draft. Applying it requires the existing explicit
		// metadata save path, with revision validation and static publication.
		jsonResponse(w, 200, result)
	})
	m.HandleFunc("POST /api/books", func(w http.ResponseWriter, r *http.Request) {
		var in store.Input
		if e := decode(w, r, &in); e != nil {
			failure(w, e)
			return
		}
		if in.ISBN != "" {
			v, e := library.ISBN(in.ISBN)
			if e != nil {
				failure(w, e)
				return
			}
			in.ISBN = v
		}
		b, e := s.Store.Save(r.Context(), in)
		if e != nil {
			failure(w, e)
			return
		}
		s.publish(w, r, b)
	})
	m.HandleFunc("PATCH /api/books/{id}", func(w http.ResponseWriter, r *http.Request) {
		var in store.Input
		if e := decode(w, r, &in); e != nil {
			failure(w, e)
			return
		}
		if in.ISBN != "" {
			v, e := library.ISBN(in.ISBN)
			if e != nil {
				failure(w, e)
				return
			}
			in.ISBN = v
		}
		b, e := s.Store.Edit(r.Context(), id(r), in)
		if e != nil {
			failure(w, e)
			return
		}
		s.publish(w, r, b)
	})
	m.HandleFunc("POST /api/books/{id}/copies", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Shelf *int64 `json:"shelf_id"`
			Key   string `json:"request_key"`
		}
		if e := decode(w, r, &in); e != nil {
			failure(w, e)
			return
		}
		b, e := s.Store.AddCopy(r.Context(), id(r), in.Shelf, in.Key)
		if e != nil {
			failure(w, e)
			return
		}
		jsonResponse(w, 200, b)
	})
	m.HandleFunc("PATCH /api/copies/{id}", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Shelf    *int64 `json:"shelf_id"`
			Revision int    `json:"revision"`
		}
		if e := decode(w, r, &in); e != nil {
			failure(w, e)
			return
		}
		if e := s.Store.MoveCopy(r.Context(), id(r), in.Shelf, in.Revision); e != nil {
			failure(w, e)
			return
		}
		jsonResponse(w, 200, map[string]bool{"saved": true})
	})
	m.HandleFunc("GET /api/shelves", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Store.Shelves(r.Context())
		if e != nil {
			failure(w, e)
			return
		}
		jsonResponse(w, 200, v)
	})
	m.HandleFunc("POST /api/shelves", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Bookcase string `json:"bookcase"`
			Name     string `json:"name"`
		}
		if e := decode(w, r, &in); e != nil {
			failure(w, e)
			return
		}
		if e := s.Store.AddShelf(r.Context(), in.Bookcase, in.Name); e != nil {
			failure(w, e)
			return
		}
		v, e := s.Store.Shelves(r.Context())
		if e != nil {
			failure(w, e)
			return
		}
		jsonResponse(w, 200, v)
	})
	m.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, 404, map[string]string{"error": "API route not found"})
	})
	m.HandleFunc("GET /book/", func(w http.ResponseWriter, r *http.Request) {
		bookID, e := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
		if e != nil || bookID < 1 {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, fmt.Sprintf("/books/%d/", bookID), http.StatusPermanentRedirect)
	})
	m.HandleFunc("GET /books/{id}/", func(w http.ResponseWriter, r *http.Request) {
		if _, e := s.Store.Book(r.Context(), id(r)); e != nil {
			http.Error(w, "Book not found", 404)
			return
		}
		if s.Pages == nil {
			http.Error(w, "Book page generation is not configured", 503)
			return
		}
		if e := s.Pages.Ensure(r.Context()); e != nil {
			http.Error(w, "Book page generation failed. Your record is saved; retry or check the local build setup.", 503)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, filepath.Join(s.Pages.Directory(), "books", strconv.FormatInt(id(r), 10), "index.html"))
	})
	m.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		web := s.Web
		if s.Pages != nil {
			web = s.Pages.Directory()
		}
		if strings.HasSuffix(r.URL.Path, "/") || strings.HasSuffix(r.URL.Path, ".html") {
			w.Header().Set("Cache-Control", "no-cache")
		}
		http.FileServer(http.Dir(web)).ServeHTTP(w, r)
	}))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, e := net.SplitHostPort(r.Host)
		if e != nil {
			host = r.Host
		}
		if host != "127.0.0.1" && host != "localhost" && host != "::1" {
			jsonResponse(w, 403, map[string]string{"error": "Local access only"})
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, e := url.Parse(origin)
			if e != nil || u.Host != r.Host || u.Scheme != "http" {
				jsonResponse(w, 403, map[string]string{"error": "Same-origin access required"})
				return
			}
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			s.ActiveWrites.Add(1)
			defer s.ActiveWrites.Add(-1)
			if s.MaintenancePath != "" {
				if _, err := os.Stat(s.MaintenancePath); err == nil {
					jsonResponse(w, 503, map[string]string{"error": "The library is updating. Your changes have not been saved; please retry when it is ready."})
					return
				} else if !os.IsNotExist(err) {
					jsonResponse(w, 503, map[string]string{"error": "The library cannot confirm update status. Please retry later."})
					return
				}
			}
		}
		m.ServeHTTP(w, r)
	})
}
func Listen(addr string) error {
	host, _, e := net.SplitHostPort(addr)
	if e != nil {
		return e
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("listen address must be a loopback IP")
	}
	return nil
}
