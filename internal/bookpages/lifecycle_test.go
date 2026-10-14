package bookpages

import (
	"context"
	"errors"
	"home-library/internal/store"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func emptyOutput(out string) error {
	if err := os.MkdirAll(out, 0700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(out, "index.html"), []byte("complete"), 0600)
}

func TestShutdownCancelsBuildAndPreservesGeneration(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "library.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	lifetime, stop := context.WithCancel(context.Background())
	defer stop()
	g, err := New(s, t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g.Lifecycle = lifetime
	g.Run = func(_ context.Context, _, out string) error { return emptyOutput(out) }
	if err := g.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	original, err := os.Readlink(g.Directory())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(context.Background(), store.Input{Title: "Committed before shutdown", RequestKey: "lifecycle-fixture"}); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	g.Run = func(ctx context.Context, _, out string) error {
		close(started)
		<-ctx.Done()
		// Even a runner returning apparently complete output must not publish after shutdown.
		return emptyOutput(out)
	}
	done := make(chan error, 1)
	go func() { done <- g.Ensure(context.Background()) }()
	<-started
	stop()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("shutdown result: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not cancel build")
	}
	current, err := os.Readlink(g.Directory())
	if err != nil || current != original {
		t.Fatalf("previous generation lost: %s %v", current, err)
	}
	generations, err := filepath.Glob(filepath.Join(g.Root, "generation-*"))
	if err != nil || len(generations) != 1 {
		t.Fatalf("abandoned stage retained: %v %v", generations, err)
	}
	books, total, err := s.List(context.Background(), "", 0, "", 1)
	if err != nil || total != 1 || books[0].Count != 1 {
		t.Fatalf("committed record changed: %v %d %v", books, total, err)
	}
}

func TestBrowserCancellationDoesNotCancelCommittedPublication(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "library.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	g, err := New(s, t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	lifetime, stop := context.WithCancel(context.Background())
	defer stop()
	g.Lifecycle = lifetime
	request, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	release := make(chan struct{})
	g.Run = func(ctx context.Context, _, out string) error {
		close(started)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-release:
			return emptyOutput(out)
		}
	}
	done := make(chan error, 1)
	go func() { done <- g.Ensure(request) }()
	<-started
	cancel()
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("browser disconnected aborted publication: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("publication did not finish")
	}
	if _, err := os.Stat(filepath.Join(g.Directory(), "index.html")); err != nil {
		t.Fatal(err)
	}
}
