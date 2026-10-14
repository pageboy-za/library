package backup

import (
	"context"
	"database/sql"
	"encoding/json"
	"home-library/internal/store"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func healthTemp(t *testing.T) string {
	t.Helper()
	path, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	return path
}
func healthSnapshot(t *testing.T, root, name string, created time.Time) string {
	t.Helper()
	source := healthTemp(t)
	s, e := store.Open(filepath.Join(source, "library.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.DB.Close()
	if _, e = s.Save(context.Background(), store.Input{Title: "Backup health book", Authors: []string{}, RequestKey: "health-fixture-book"}); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(root, name)
	if e = Create(context.Background(), source, path); e != nil {
		t.Fatal(e)
	}
	healthCreated(t, path, created)
	return path
}
func healthCreated(t *testing.T, path string, created time.Time) {
	t.Helper()
	raw, e := os.ReadFile(filepath.Join(path, "manifest.json"))
	if e != nil {
		t.Fatal(e)
	}
	var m Manifest
	if e = json.Unmarshal(raw, &m); e != nil {
		t.Fatal(e)
	}
	m.CreatedAt = created.UTC().Format(time.RFC3339)
	if e = writeJSON(filepath.Join(path, "manifest.json"), m); e != nil {
		t.Fatal(e)
	}
	if e = os.Chtimes(path, created, created); e != nil {
		t.Fatal(e)
	}
}
func healthAt(root string, now time.Time) *HealthService {
	h := NewHealthService(root)
	h.now = func() time.Time { return now }
	return h
}
func TestHealthUnconfiguredMissingAndVerified(t *testing.T) {
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	ctx := context.Background()
	empty := healthAt("", now).Status(ctx)
	if empty.Configured || empty.State != "unconfigured" {
		t.Fatalf("Unconfigured %+v", empty)
	}
	root := healthTemp(t)
	missing := healthAt(filepath.Join(root, "missing"), now).Status(ctx)
	if !missing.Configured || missing.State != "failed" {
		t.Fatalf("Missing %+v", missing)
	}
	if emptyFolder := healthAt(root, now).Status(ctx); emptyFolder.State != "no_backups" {
		t.Fatal(emptyFolder)
	}
	healthSnapshot(t, root, "snapshot", now.Add(-time.Hour))
	status := healthAt(root, now).Status(ctx)
	if status.State != "verified" || status.BackupCount != 1 || status.Stale || status.LastVerifiedAt != now.Add(-time.Hour).Format(time.RFC3339) {
		t.Fatalf("Verified %+v", status)
	}
	raw, _ := json.Marshal(status)
	if strings.Contains(string(raw), root) {
		t.Fatal("Status leaks filesystem path")
	}
	oldRoot := healthTemp(t)
	healthSnapshot(t, oldRoot, "old", now.Add(-49*time.Hour))
	old := healthAt(oldRoot, now).Status(ctx)
	if old.State != "verified" || !old.Stale {
		t.Fatalf("Old verified %+v", old)
	}
}
func TestHealthLatestFailureDoesNotHideBehindOlderGood(t *testing.T) {
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	root := healthTemp(t)
	old := now.Add(-time.Hour)
	healthSnapshot(t, root, "older", old)
	recent := healthSnapshot(t, root, "newest", now.Add(-time.Minute))
	if e := os.WriteFile(filepath.Join(recent, "catalogue.json"), []byte("corrupt"), 0600); e != nil {
		t.Fatal(e)
	}
	out := healthAt(root, now).Status(context.Background())
	if out.State != "failed" || out.LastVerifiedAt != old.Format(time.RFC3339) || out.BackupCount != 2 {
		t.Fatalf("Corruption hidden %+v", out)
	}
	unfinished := filepath.Join(root, "unfinished")
	os.Mkdir(unfinished, 0700)
	os.Chtimes(unfinished, now, now)
	out = healthAt(root, now).Status(context.Background())
	if out.State != "failed" || out.LastAttemptAt != now.Format(time.RFC3339) || out.BackupCount != 3 {
		t.Fatalf("Unfinished hidden %+v", out)
	}
}
func TestHealthFutureAndMalformedCreationDate(t *testing.T) {
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	for _, created := range []string{now.Add(time.Hour).Format(time.RFC3339), "not-a-date", ""} {
		t.Run(created, func(t *testing.T) {
			root := healthTemp(t)
			path := healthSnapshot(t, root, "snapshot", now)
			raw, _ := os.ReadFile(filepath.Join(path, "manifest.json"))
			var m Manifest
			json.Unmarshal(raw, &m)
			m.CreatedAt = created
			writeJSON(filepath.Join(path, "manifest.json"), m)
			out := healthAt(root, now).Status(context.Background())
			if out.State != "failed" || out.LastVerifiedAt != "" {
				t.Fatalf("Invalid date accepted %+v", out)
			}
		})
	}
}
func TestHealthSymlinksNeverFollowed(t *testing.T) {
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	root := healthTemp(t)
	good := healthSnapshot(t, root, "old-good", now.Add(-time.Hour))
	links := healthTemp(t)
	rootLink := filepath.Join(links, "linked-root")
	os.Symlink(root, rootLink)
	if out := healthAt(rootLink, now).Status(context.Background()); out.State != "failed" {
		t.Fatal("Followed root link")
	}
	snapshotLink := filepath.Join(root, "newer-link")
	os.Symlink(good, snapshotLink)
	if out := healthAt(root, now.Add(24*time.Hour)).Status(context.Background()); out.State != "failed" {
		t.Fatalf("Newer unsafe entry hidden %+v", out)
	}
	os.Remove(snapshotLink)
	bad := filepath.Join(root, "newest")
	os.Mkdir(bad, 0700)
	os.Symlink(filepath.Join(good, "manifest.json"), filepath.Join(bad, "manifest.json"))
	os.Chtimes(bad, now, now)
	if out := healthAt(root, now).Status(context.Background()); out.State != "failed" {
		t.Fatal("Followed manifest symlink")
	}
	os.RemoveAll(bad)
	bad = healthSnapshot(t, root, "newest", now)
	os.Remove(filepath.Join(bad, "library.sqlite"))
	os.Symlink(filepath.Join(good, "library.sqlite"), filepath.Join(bad, "library.sqlite"))
	if out := healthAt(root, now).Status(context.Background()); out.State != "failed" {
		t.Fatal("Followed database symlink")
	}
}
func TestHealthCacheConcurrencyAndLimits(t *testing.T) {
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	root := healthTemp(t)
	path := healthSnapshot(t, root, "snapshot", now)
	h := healthAt(root, now)
	if out := h.Status(context.Background()); out.State != "verified" {
		t.Fatal(out)
	}
	os.WriteFile(filepath.Join(path, "catalogue.json"), []byte("now corrupt"), 0600)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if out := h.Status(context.Background()); out.State != "verified" {
				t.Error("Cached status unexpectedly changed")
			}
		}()
	}
	wg.Wait()
	h.now = func() time.Time { return now.Add(31 * time.Second) }
	if out := h.Status(context.Background()); out.State != "failed" {
		t.Fatal("Verification cache never expired")
	}
	budgetRoot := healthTemp(t)
	snapshot := healthSnapshot(t, budgetRoot, "huge", now)
	raw, _ := os.ReadFile(filepath.Join(snapshot, "manifest.json"))
	var m Manifest
	json.Unmarshal(raw, &m)
	m.Files = append(m.Files, File{Path: "huge.bin", Bytes: statusMaxFileBytes + 1, SHA256: "unused"})
	file, e := os.Create(filepath.Join(snapshot, "huge.bin"))
	if e != nil {
		t.Fatal(e)
	}
	file.Truncate(statusMaxFileBytes + 1)
	file.Close()
	writeJSON(filepath.Join(snapshot, "manifest.json"), m)
	out := healthAt(budgetRoot, now).Status(context.Background())
	if out.State != "failed" || !strings.Contains(out.Message, "limits") {
		t.Fatalf("Budget not enforced %+v", out)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if out := healthAt(root, now).Status(canceled); out.State != "failed" {
		t.Fatal("Canceled verification reported healthy")
	}
}
func TestHealthIgnoresOrdinaryRunnerLockButRejectsSymlinkLock(t *testing.T) {
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	root := healthTemp(t)
	healthSnapshot(t, root, "snapshot", now.Add(-time.Hour))
	lock := filepath.Join(root, ".library-backup.lock")
	if e := os.Mkdir(lock, 0700); e != nil {
		t.Fatal(e)
	}
	out := healthAt(root, now).Status(context.Background())
	if out.State != "verified" || out.BackupCount != 1 {
		t.Fatalf("Internal lock counted %+v", out)
	}
	os.Remove(lock)
	os.Symlink(healthTemp(t), lock)
	out = healthAt(root, now.Add(24*time.Hour)).Status(context.Background())
	if out.State != "failed" {
		t.Fatal("Unsafe lock link followed or hidden")
	}
}
func TestHealthCandidateScanLimit(t *testing.T) {
	root := healthTemp(t)
	for i := 0; i < statusMaxEntries+1; i++ {
		if e := os.Mkdir(filepath.Join(root, time.Unix(int64(i), 0).UTC().Format("20060102T150405")), 0700); e != nil {
			t.Fatal(e)
		}
	}
	out := NewHealthService(root).Status(context.Background())
	if out.State != "failed" || out.BackupCount > statusMaxEntries || !strings.Contains(out.Message, "Too many") {
		t.Fatalf("Scan limit not enforced %+v", out)
	}
}

func TestValidHashOrphanFailsVerificationHealthAndRestore(t *testing.T) {
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	root := healthTemp(t)
	snapshot := healthSnapshot(t, root, "orphan", now.Add(-time.Hour))
	database := filepath.Join(snapshot, "library.sqlite")
	db, err := sql.Open("sqlite", database)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec("INSERT INTO copies(book_id) VALUES(999999)")
	closeErr := db.Close()
	if err != nil {
		t.Fatal(err)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	raw, err := os.ReadFile(filepath.Join(snapshot, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest Manifest
	if err = json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	updated, err := digest(database)
	if err != nil {
		t.Fatal(err)
	}
	for i, file := range manifest.Files {
		if file.Path == "library.sqlite" {
			updated.Path = file.Path
			manifest.Files[i] = updated
		}
	}
	if err = writeJSON(filepath.Join(snapshot, "manifest.json"), manifest); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err = Verify(ctx, snapshot); err == nil || !strings.Contains(err.Error(), "foreign key violation") {
		t.Fatalf("Orphan verification: %v", err)
	}
	status := healthAt(root, now).Status(ctx)
	if status.State != "failed" || status.LastVerifiedAt != "" {
		t.Fatalf("Orphan health: %+v", status)
	}
	destination := filepath.Join(healthTemp(t), "restored")
	if err = Restore(ctx, snapshot, destination); err == nil {
		t.Fatal("Orphan restored")
	}
	if _, err = os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("Restore created destination: %v", err)
	}
}
