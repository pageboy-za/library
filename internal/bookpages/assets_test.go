package bookpages

import (
	"context"
	"home-library/internal/store"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAssetsSurviveFrontendPublicationAndCollision(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	g, _ := New(s, t.TempDir(), t.TempDir())
	asset, body := "island.old123.js", "old island"
	g.Run = func(_ context.Context, snapshot, out string) error {
		if err := os.MkdirAll(filepath.Join(out, "_astro"), 0700); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(out, "index.html"), []byte(asset), 0600); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(out, "_astro", asset), []byte(body), 0600)
	}
	if err := g.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	asset, body = "island.new456.js", "new island"
	g.fingerprint = ""
	if err := g.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	old, err := os.ReadFile(filepath.Join(g.AssetsDirectory(), "island.old123.js"))
	if err != nil || string(old) != "old island" {
		t.Fatalf("Older tab lost its bundle: %s %v", old, err)
	}
	current, _ := os.Readlink(g.Directory())
	body = "changed same name"
	g.fingerprint = ""
	if err := g.Ensure(context.Background()); err == nil {
		t.Fatal("Conflicting immutable asset was published")
	}
	after, _ := os.Readlink(g.Directory())
	if after != current {
		t.Fatal("Failed asset preservation replaced live site")
	}
	data, _ := os.ReadFile(filepath.Join(g.AssetsDirectory(), asset))
	if string(data) != "new island" {
		t.Fatal("Live bundle overwritten")
	}
}

func TestPruneKeepsCurrentPredecessorsAndGracePeriod(t *testing.T) {
	g := &Generator{Root: t.TempDir()}
	now := time.Now()
	for i, name := range []string{"generation-current", "generation-previous1", "generation-previous2", "generation-recent", "generation-old"} {
		dir := filepath.Join(g.Root, name)
		os.MkdirAll(filepath.Join(dir, "site"), 0700)
		stamp := now.Add(-time.Duration(i+1) * time.Hour)
		if name == "generation-recent" {
			stamp = now.Add(-10 * time.Minute)
		}
		os.Chtimes(dir, stamp, stamp)
	}
	// Current stays even when its timestamp is older than every predecessor.
	current := filepath.Join(g.Root, "generation-current")
	os.Chtimes(current, now.Add(-24*time.Hour), now.Add(-24*time.Hour))
	os.Symlink(filepath.Join(current, "site"), g.Directory())
	os.Mkdir(filepath.Join(g.Root, "household-data"), 0700)
	if err := g.pruneGenerations(now); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"generation-current", "generation-recent", "generation-previous1", "household-data"} {
		if _, err := os.Stat(filepath.Join(g.Root, name)); err != nil {
			t.Errorf("Removed protected directory %s", name)
		}
	}
	if _, err := os.Stat(filepath.Join(g.Root, "generation-old")); !os.IsNotExist(err) {
		t.Fatal("Old generation not reclaimed")
	}
}
