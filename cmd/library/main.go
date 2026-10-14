package main

import (
	"context"
	"flag"
	"home-library/internal/bookpages"
	"home-library/internal/covers"
	"home-library/internal/httpapi"
	"home-library/internal/metadata"
	"home-library/internal/store"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func main() {
	addr := flag.String("listen", "127.0.0.1:8080", "loopback address")
	data := flag.String("data", "data", "persistent data directory")
	web := flag.String("web", "web", "Astro source directory with installed dependencies")
	backupDirectory := flag.String("backup-dir", "", "optional backup parent directory for read-only health checks")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if e := httpapi.Listen(*addr); e != nil {
		log.Fatal(e)
	}
	if e := os.MkdirAll(*data, 0700); e != nil {
		log.Fatal(e)
	}
	s, e := store.Open(filepath.Join(*data, "library.sqlite"))
	if e != nil {
		log.Fatal(e)
	}
	defer s.DB.Close()
	pages, e := bookpages.New(s, *web, filepath.Join(*data, "pages"))
	if e != nil {
		log.Fatal(e)
	}
	pages.Lifecycle = ctx
	coverService := covers.New(s, filepath.Join(*data, "covers"))
	log.Print("Generating static Astro pages from library metadata…")
	if e = pages.Ensure(ctx); e != nil {
		if ctx.Err() != nil {
			return
		}
		if _, existing := os.Stat(filepath.Join(pages.Directory(), "index.html")); existing != nil {
			log.Fatal(e)
		}
		log.Printf("Using previous static output; regeneration needs repair: %v", e)
	}
	srv := &http.Server{Addr: *addr, Handler: (&httpapi.Server{Store: s, Metadata: metadata.NewWithCache(filepath.Join(*data, "metadata")), Web: *web, BackupDirectory: *backupDirectory, Pages: pages, Covers: coverService}).Handler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	go func() {
		<-ctx.Done()
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(c)
	}()
	// Missing-cover backfill never delays readiness. Bound the whole startup pass
	// so offline catalogues do not trigger a long sequence of remote timeouts.
	go func() {
		c, cancel := context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
		for page := 1; ; page++ {
			books, _, err := s.List(c, "", 0, "", page)
			if err != nil || len(books) == 0 {
				return
			}
			for _, b := range books {
				if c.Err() != nil {
					return
				}
				if b.CoverURL == "" && b.ISBN != "" {
					if _, err = coverService.Fetch(c, b.ID); err != nil {
						log.Printf("Cover lookup for %s: %v", b.Title, err)
						continue
					}
					if err = pages.Ensure(c); err != nil {
						log.Printf("Publish cover for book %d: %v", b.ID, err)
					}
				}
			}
		}
	}()
	log.Printf("Home Library http://%s (data: %s)", *addr, *data)
	if e = srv.ListenAndServe(); e != nil && e != http.ErrServerClosed {
		log.Fatal(e)
	}
}
