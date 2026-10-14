package backup

import (
	"context"
	"home-library/internal/store"
	"path/filepath"
	"testing"
)

func TestDeletedBookBackupCanRestoreOriginalCopies(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	s, e := store.Open(filepath.Join(source, "library.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.DB.Close()
	subtitle := "Retained subtitle"
	b, e := s.Save(ctx, store.Input{Title: "Deleted backup fixture", Subtitle: &subtitle, RequestKey: "backup-delete-save"})
	if e != nil {
		t.Fatal(e)
	}
	deleted, e := s.SetBookDeleted(ctx, b.ID, b.Revision, true, "backup-delete-book")
	if e != nil {
		t.Fatal(e)
	}
	snapshot := filepath.Join(t.TempDir(), "snapshot")
	if e = Create(ctx, source, snapshot); e != nil {
		t.Fatal(e)
	}
	if e = Verify(ctx, snapshot); e != nil {
		t.Fatal(e)
	}
	destination := filepath.Join(t.TempDir(), "restored")
	if e = Restore(ctx, snapshot, destination); e != nil {
		t.Fatal(e)
	}
	restored, e := store.Open(filepath.Join(destination, "library.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer restored.DB.Close()
	recovered, e := restored.SetBookDeleted(ctx, b.ID, deleted.Revision, false, "backup-restore-book")
	if e != nil || recovered.Subtitle != subtitle || recovered.Count != 1 || recovered.Copies[0].ID != b.Copies[0].ID {
		t.Fatalf("Recovery lost copy identity %+v %v", recovered, e)
	}
}
