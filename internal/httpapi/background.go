package httpapi

import (
	"context"
	"home-library/internal/metrics"
	"log"
	"os"
	"time"
)

// Queue only derived work: committed records remain authoritative. Startup
// generation and missing-cover backfill recover work interrupted by a restart.
func (s *Server) queuePublication(bookID int64) {
	s.backgroundMu.Lock()
	defer s.backgroundMu.Unlock()
	if s.backgroundBooks == nil {
		s.backgroundBooks = make(map[int64]struct{})
	}
	s.backgroundBooks[bookID] = struct{}{}
	if s.backgroundRunning {
		return
	}
	s.backgroundRunning = true
	s.ActiveWrites.Add(1) // Updates must wait for derived file writes too.
	go s.publishBackground()
}

func (s *Server) publishBackground() {
	defer s.ActiveWrites.Add(-1)
	lifecycle := s.Lifecycle
	if lifecycle == nil {
		lifecycle = context.Background()
	}
	// Coalesce rapid saves rather than launching a build for each scanner input.
	timer := time.NewTimer(200 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-lifecycle.Done():
		s.finishBackground()
		return
	case <-timer.C:
	}
	for {
		s.backgroundMu.Lock()
		if len(s.backgroundBooks) == 0 || lifecycle.Err() != nil || s.backgroundMaintenance() {
			s.backgroundRunning = false
			s.backgroundMu.Unlock()
			return
		}
		pending := s.backgroundBooks
		s.backgroundBooks = make(map[int64]struct{})
		s.backgroundMu.Unlock()
		ctx, trace := metrics.Start(lifecycle)
		for id := range pending {
			if lifecycle.Err() != nil || s.backgroundMaintenance() {
				break
			}
			b, err := s.Store.Book(ctx, id)
			if err != nil || b.CoverURL != "" || b.ISBN == "" || s.Covers == nil {
				continue
			}
			coverCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			err = metrics.Measure(ctx, "background_cover", func() error { _, e := s.Covers.Fetch(coverCtx, id); return e })
			cancel()
			if err != nil {
				log.Printf("Background cover lookup failed (%T)", err)
			}
		}
		if s.Pages != nil && lifecycle.Err() == nil && !s.backgroundMaintenance() {
			// A failed build never changes the live shell or the saved record. Later
			// saves and startup retry generation; keep failures out of save responses.
			if err := metrics.Measure(ctx, "background_publication", func() error { return s.Pages.Ensure(ctx) }); err != nil {
				log.Printf("Background page generation failed (%T)", err)
			}
		}
		trace.Finish()
	}
}
func (s *Server) finishBackground() {
	s.backgroundMu.Lock()
	s.backgroundRunning = false
	s.backgroundMu.Unlock()
}

// Yield to updater maintenance between bounded operations. Unfinished derived
// work is reconstructed at startup from the committed database.
func (s *Server) backgroundMaintenance() bool {
	if s.MaintenancePath == "" {
		return false
	}
	_, err := os.Stat(s.MaintenancePath)
	return !os.IsNotExist(err)
}
