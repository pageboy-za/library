package main

import (
	"context"
	"flag"
	"home-library/internal/auth"
	"home-library/internal/covers"
	"home-library/internal/httpapi"
	"home-library/internal/membership"
	"home-library/internal/metadata"
	"home-library/internal/store"
	"home-library/internal/wallet"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

var version = "development"
var commit = "unknown"

func main() {
	addr := flag.String("listen", "127.0.0.1:8080", "loopback address")
	data := flag.String("data", "data", "persistent data directory")
	web := flag.String("web", "web/dist", "prebuilt frontend directory")
	backupDirectory := flag.String("backup-dir", "", "optional backup parent directory for read-only health checks")
	maintenance := flag.String("maintenance-file", "", "read-only updater maintenance marker")
	secureCookies := flag.Bool("secure-cookies", true, "require HTTPS for staff session cookies; disable only for local HTTP development")
	walletPath := flag.String("wallet-config", os.Getenv("LIBRARY_WALLET_CONFIG"), "optional private Wallet issuer configuration file")
	publicDefault := os.Getenv("LIBRARY_PUBLIC_URL")
	if publicDefault == "" {
		publicDefault = "https://library.local"
	}
	publicURL := flag.String("public-url", publicDefault, "private HTTPS origin used by the registration QR code")
	flag.Parse()
	if _, e := httpapi.RegistrationURL(*publicURL); e != nil {
		log.Fatal(e)
	}
	walletService, e := wallet.NewFromFile(*walletPath)
	if e != nil {
		log.Fatal(e)
	}
	if e := walletService.ValidateLocations(*web, *data); e != nil {
		log.Fatal(e)
	}
	if err := os.Setenv("LIBRARY_APP_VERSION", version); err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if e := httpapi.Listen(*addr); e != nil {
		log.Fatal(e)
	}
	// Validate the complete frontend before opening the listener. No compiler or
	// household-derived HTML is needed at runtime.
	for _, required := range []string{"index.html", "book-view/index.html", "settings/index.html", "scan/index.html", "join/index.html", "membership/index.html", "staff/login/index.html", "staff/members/index.html"} {
		info, err := os.Stat(filepath.Join(*web, required))
		if err != nil || !info.Mode().IsRegular() {
			log.Fatalf("Missing prebuilt frontend %s; build the frontend or install a complete release", required)
		}
	}
	if e := os.MkdirAll(*data, 0700); e != nil {
		log.Fatal(e)
	}
	s, e := store.Open(filepath.Join(*data, "library.sqlite"))
	if e != nil {
		log.Fatal(e)
	}
	defer s.DB.Close()
	coverService := covers.New(s, filepath.Join(*data, "covers"))
	apiServer := &httpapi.Server{Access: auth.New(s.DB), Membership: membership.New(s.DB), Wallet: walletService, PublicURL: *publicURL, SecureCookies: *secureCookies, Lifecycle: ctx, Store: s, Metadata: metadata.NewWithCache(filepath.Join(*data, "metadata")), Web: *web, BackupDirectory: *backupDirectory, Covers: coverService, Version: version, Commit: commit, MaintenancePath: *maintenance}
	srv := &http.Server{Addr: *addr, Handler: apiServer.Handler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	go func() {
		<-ctx.Done()
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(c)
	}()
	// Missing-cover backfill never delays readiness. Bound the whole startup pass
	// so offline catalogues do not trigger a long sequence of remote timeouts.
	go func() {
		apiServer.ActiveWrites.Add(1)
		defer apiServer.ActiveWrites.Add(-1)
		c, cancel := context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
		for page := 1; ; page++ {
			if *maintenance != "" {
				if _, err := os.Stat(*maintenance); err == nil {
					return
				}
			}
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
						log.Printf("Cover lookup for book %d failed: %T", b.ID, err)
						continue
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
