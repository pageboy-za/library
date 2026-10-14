package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"home-library/internal/store"
	"os"
	"path/filepath"
	"testing"
)

func TestLiveWALSnapshotExportAndRestore(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	s, e := store.Open(filepath.Join(source, "library.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.DB.Close()
	if _, e = s.DB.Exec("PRAGMA wal_autocheckpoint=0"); e != nil {
		t.Fatal(e)
	}
	b, e := s.Save(ctx, store.Input{Title: "Book still in WAL", Authors: []string{"An Author"}, RequestKey: "snapshot-token"})
	if e != nil {
		t.Fatal(e)
	}
	bytes := []byte("immutable cover bytes")
	hash := sha256.Sum256(bytes)
	checksum := hex.EncodeToString(hash[:])
	name := checksum + ".png"
	os.Mkdir(filepath.Join(source, "covers"), 0700)
	if e = os.WriteFile(filepath.Join(source, "covers", name), bytes, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = s.SaveCover(ctx, store.CoverAsset{BookID: b.ID, LocalPath: name, Checksum: checksum, SourceKind: "upload"}, true, false); e != nil {
		t.Fatal(e)
	}
	info, e := os.Stat(filepath.Join(source, "library.sqlite-wal"))
	if e != nil || info.Size() == 0 {
		t.Fatal("test needs committed live WAL", e)
	}
	directory := filepath.Join(t.TempDir(), "snapshot")
	if e = Create(ctx, source, directory); e != nil {
		t.Fatal(e)
	}
	if e = Verify(ctx, directory); e != nil {
		t.Fatal(e)
	}
	db, e := openRead(filepath.Join(directory, "library.sqlite"), true)
	if e != nil {
		t.Fatal(e)
	}
	var title string
	if e = db.QueryRow("SELECT title FROM books WHERE id=?", b.ID).Scan(&title); e != nil || title != "Book still in WAL" {
		t.Fatal(title, e)
	}
	db.Close()
	raw, _ := os.ReadFile(filepath.Join(directory, "catalogue.json"))
	var exported Export
	if e = json.Unmarshal(raw, &exported); e != nil {
		t.Fatal(e)
	}
	if len(exported.Tables["books"]) != 1 || exported.Tables["mutation_requests"] != nil {
		t.Fatal(exported)
	}
	authors := exported.Tables["books"][0]["authors"].([]any)
	if authors[0] != "An Author" {
		t.Fatal(authors)
	}
	if e = Create(ctx, source, directory); e == nil {
		t.Fatal("overwrite allowed")
	}
	restore := filepath.Join(t.TempDir(), "restored")
	if e = Restore(ctx, directory, restore); e != nil {
		t.Fatal(e)
	}
	if e = Restore(ctx, directory, restore); e == nil {
		t.Fatal("restore overwrite allowed")
	}
	restored, e := store.Open(filepath.Join(restore, "library.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer restored.DB.Close()
	got, e := restored.Book(ctx, b.ID)
	if e != nil || got.Title != b.Title || got.Count != 1 {
		t.Fatal(got, e)
	}
	if e = os.WriteFile(filepath.Join(directory, "covers", name), []byte("corrupt"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = Verify(ctx, directory); e == nil {
		t.Fatal("corrupt image not detected")
	}
	if e = Restore(ctx, directory, filepath.Join(t.TempDir(), "bad")); e == nil {
		t.Fatal("corrupt backup restored")
	}
}
func TestMissingCoverFailsAndCleansDestination(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	s, e := store.Open(filepath.Join(source, "library.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.DB.Close()
	b, e := s.Save(ctx, store.Input{Title: "Missing cover", Authors: []string{}, RequestKey: "missing-cover"})
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.SaveCover(ctx, store.CoverAsset{BookID: b.ID, LocalPath: "missing.png", Checksum: "hash", SourceKind: "upload"}, true, false)
	if e != nil {
		t.Fatal(e)
	}
	dest := filepath.Join(t.TempDir(), "backup")
	if e = Create(ctx, source, dest); e == nil {
		t.Fatal("missing cover accepted")
	}
	if _, e = os.Stat(dest); !os.IsNotExist(e) {
		t.Fatal("failed destination not removed", e)
	}
}
func TestUnsafeManifest(t *testing.T) {
	dir := t.TempDir()
	writeJSON(filepath.Join(dir, "manifest.json"), Manifest{Format: 1, Files: []File{{Path: "../outside"}}})
	if e := Verify(context.Background(), dir); e == nil {
		t.Fatal("unsafe path accepted")
	}
}
