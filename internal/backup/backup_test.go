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

func TestStaffBackupPreservesAccountsButInvalidatesRestoredSessions(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	s, err := store.Open(filepath.Join(source, "library.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	// Synthetic records deliberately avoid expensive password generation: backup
	// must preserve opaque hashes exactly, without exporting them to catalogue JSON.
	_, err = s.DB.Exec(`INSERT INTO staff_users(id,username,display_name,role,password_hash,created_at,updated_at) VALUES(1,'fixture','Fixture','administrator','PRIVATE_HASH_FIXTURE',1,1);
 INSERT INTO staff_sessions(token_hash,user_id,created_at,expires_at,last_seen_at) VALUES('PRIVATE_SESSION_FIXTURE',1,1,9999999999,1)`)
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "snapshot")
	if err = Create(ctx, source, archive); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(archive, "catalogue.json"))
	if err != nil {
		t.Fatal(err)
	}
	var exported Export
	if err = json.Unmarshal(raw, &exported); err != nil {
		t.Fatal(err)
	}
	if _, ok := exported.Tables["staff_users"]; ok {
		t.Fatal("staff credentials leaked into catalogue export")
	}
	if _, ok := exported.Tables["staff_sessions"]; ok {
		t.Fatal("staff sessions leaked into catalogue export")
	}
	restored := filepath.Join(t.TempDir(), "restored")
	if err = Restore(ctx, archive, restored); err != nil {
		t.Fatal(err)
	}
	restoredDB, err := openRead(filepath.Join(restored, "library.sqlite"), false)
	if err != nil {
		t.Fatal(err)
	}
	defer restoredDB.Close()
	var hash string
	if err = restoredDB.QueryRow("SELECT password_hash FROM staff_users WHERE id=1").Scan(&hash); err != nil || hash != "PRIVATE_HASH_FIXTURE" {
		t.Fatal(hash, err)
	}
	var count int
	if err = restoredDB.QueryRow("SELECT COUNT(*) FROM staff_sessions").Scan(&count); err != nil || count != 0 {
		t.Fatal("restored sessions must be invalidated", count, err)
	}
	if err = Verify(ctx, archive); err != nil {
		t.Fatal("restore modified original backup", err)
	}
}
