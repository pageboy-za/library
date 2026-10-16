package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"home-library/internal/auth"
	"home-library/internal/catalogue"
	"home-library/internal/covers"
	"home-library/internal/lending"
	"home-library/internal/library"
	"home-library/internal/locations"
	"home-library/internal/membership"
	"home-library/internal/metadata"
	"home-library/internal/metrics"
	"home-library/internal/store"
	"home-library/internal/wallet"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

type Server struct {
	Access               *auth.Service
	Membership           *membership.Service
	Wallet               *wallet.Service
	PublicURL            string
	registrationLimiter  registrationLimiter
	memberLendingLimiter registrationLimiter
	SecureCookies        bool
	loginGuard           loginGuard
	Lifecycle            context.Context
	backgroundMu         sync.Mutex
	backgroundBooks      map[int64]struct{}
	backgroundRunning    bool
	Version              string
	Commit               string
	MaintenancePath      string
	ActiveWrites         atomic.Int64
	BackupDirectory      string
	Store                *store.Store
	Metadata             *metadata.Client
	Web                  string
	Covers               *covers.Service
	Enrichment           *catalogue.EnrichmentService
	Pages                interface {
		Ensure(context.Context) error
		Directory() string
	}
}

// Saving acknowledges only durable database work; derived covers/pages follow.
func (s *Server) publish(w http.ResponseWriter, r *http.Request, b store.Book) {
	if s.Pages != nil || s.Covers != nil {
		s.queuePublication(b.ID)
	}
	if trace := metrics.Get(r.Context()); trace != nil {
		w.Header().Set("Server-Timing", trace.Header())
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
	s.registerAccess(m)
	s.registerMember(m)
	s.registerMemberLending(m)
	s.registerMembership(m)
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
	s.registerPreferences(m)
	s.registerDeletion(m)
	s.registerSubtitles(m)
	s.registerBibliography(m)
	enrichment := s.Enrichment
	if enrichment == nil {
		enrichment = catalogue.NewEnrichment(s.Store)
	}
	m.HandleFunc("POST /api/books/{id}/discovery/enrich", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			IncludePublicationYear bool `json:"include_publication_year"`
		}
		if e := decode(w, r, &in); e != nil {
			failure(w, e)
			return
		}
		v, e := enrichment.EnrichWithOptions(r.Context(), id(r), in.IncludePublicationYear)
		if e != nil {
			failure(w, e)
			return
		}
		if v.Changed && (s.Pages != nil || s.Covers != nil) {
			s.queuePublication(id(r))
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
		b, total, e := s.Store.ListWithOptions(r.Context(), r.URL.Query().Get("q"), shelf, r.URL.Query().Get("availability"), page, store.ListOptions{Subject: r.URL.Query().Get("subject"), Sort: r.URL.Query().Get("sort")})
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
			key := "existing"
			if b.DeletedAt != nil {
				key = "deleted"
			}
			jsonResponse(w, 200, map[string]any{key: b, "isbn": isbn})
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
		var b store.Book
		e := metrics.Measure(r.Context(), "database", func() error { var err error; b, err = s.Store.Save(r.Context(), in); return err })
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
		var b store.Book
		e := metrics.Measure(r.Context(), "database", func() error { var err error; b, err = s.Store.Edit(r.Context(), id(r), in); return err })
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
	m.HandleFunc("GET /books/{id}/index.html", func(w http.ResponseWriter, r *http.Request) {
		if _, e := s.Store.Book(r.Context(), id(r)); e != nil {
			http.Error(w, "Book not found", 404)
			return
		}
		http.Redirect(w, r, fmt.Sprintf("/books/%d/", id(r)), http.StatusSeeOther)
	})
	m.HandleFunc("GET /staff/books/{id}/{$}", func(w http.ResponseWriter, r *http.Request) {
		if _, e := s.Store.Book(r.Context(), id(r)); e != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		http.ServeFile(w, r, filepath.Join(s.Web, "book-view", "index.html"))
	})
	m.HandleFunc("GET /books/{id}/{$}", func(w http.ResponseWriter, r *http.Request) {
		if _, e := s.Store.Book(r.Context(), id(r)); e != nil {
			http.Error(w, "Book not found", 404)
			return
		}
		web := s.Web
		if s.Pages != nil {
			web = s.Pages.Directory()
		}
		w.Header().Set("Cache-Control", "no-store")
		http.ServeFile(w, r, filepath.Join(web, "book-view", "index.html"))
	})
	m.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		web := s.Web
		if s.Pages != nil {
			web = s.Pages.Directory()
		}
		// Only serve actual build outputs. Unknown routes must not become an SPA
		// success response, and build directories must never expose listings.
		relative := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		file := filepath.Join(web, filepath.FromSlash(relative))
		info, err := os.Stat(file)
		if err == nil && info.IsDir() {
			file = filepath.Join(file, "index.html")
			info, err = os.Stat(file)
		}
		if err != nil || !info.Mode().IsRegular() {
			http.NotFound(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/_app/immutable/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			// In particular, HTML and SvelteKit's version.json must revalidate
			// after an application upgrade.
			w.Header().Set("Cache-Control", "no-cache")
		}
		http.FileServer(http.Dir(web)).ServeHTTP(w, r)
	})
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
		if (r.Method == http.MethodPost || r.Method == http.MethodPatch) && strings.HasPrefix(r.URL.Path, "/api/books") {
			ctx, trace := metrics.Start(r.Context())
			r = r.WithContext(ctx)
			w.Header().Set("X-Library-Request-ID", trace.ID)
			defer trace.Finish()
		}
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
		if authorized, ok := s.accessRequest(w, r); ok {
			m.ServeHTTP(w, authorized)
		}
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
