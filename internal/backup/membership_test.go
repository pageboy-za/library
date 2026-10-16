package backup

import (
	"context"
	"encoding/json"
	"home-library/internal/membership"
	"home-library/internal/store"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMemberBackupPreservesPrivateStateWithoutPortableContactExport(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	s, err := store.Open(filepath.Join(source, "library.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	// Account hashing is opaque to backup; use a disposable synthetic reviewer.
	if _, err = s.DB.Exec(`INSERT INTO staff_users(id,username,display_name,role,password_hash,created_at,updated_at) VALUES(1,'reviewer','Fixture reviewer','administrator','fixture-password-hash',1,1)`); err != nil {
		t.Fatal(err)
	}
	service := membership.New(s.DB)
	registration, token, err := service.Register(ctx, "Backup member fixture", "private-member-fixture@example.org", "+44 1234 567890")
	if err != nil {
		t.Fatal(err)
	}
	approved, err := service.Approve(ctx, registration.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	var receiptHash string
	if err = s.DB.QueryRow("SELECT receipt_token_hash FROM member_registrations WHERE id=?", approved.ID).Scan(&receiptHash); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "snapshot")
	if err = Create(ctx, source, archive); err != nil {
		t.Fatal(err)
	}
	if err = Verify(ctx, archive); err != nil {
		t.Fatal(err)
	}
	portable, err := os.ReadFile(filepath.Join(archive, "catalogue.json"))
	if err != nil {
		t.Fatal(err)
	}
	var exported Export
	if err = json.Unmarshal(portable, &exported); err != nil {
		t.Fatal(err)
	}
	if _, exists := exported.Tables["member_registrations"]; exists {
		t.Fatal("private membership table leaked into readable catalogue export")
	}
	// Borrower display names remain part of the ordinary catalogue export. Private
	// registration contacts and receipt/card capabilities do not belong there.
	for _, secret := range []string{registration.Email, registration.Telephone, receiptHash, token, approved.CardCode} {
		if strings.Contains(string(portable), secret) {
			t.Fatalf("private membership value leaked into portable export: %q", secret)
		}
	}
	restored := filepath.Join(t.TempDir(), "restored")
	if err = Restore(ctx, archive, restored); err != nil {
		t.Fatal(err)
	}
	recoveredStore, err := store.Open(filepath.Join(restored, "library.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer recoveredStore.DB.Close()
	recovered, err := membership.New(recoveredStore.DB).Receipt(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Name != registration.Name || recovered.Email != registration.Email || recovered.Telephone != registration.Telephone || recovered.Status != "approved" || recovered.CardCode != approved.CardCode || recovered.BorrowerID == nil || *recovered.BorrowerID != *approved.BorrowerID {
		t.Fatalf("private SQLite membership continuity lost: %+v", recovered)
	}
	if recovered.ReviewedAt == nil || *recovered.ReviewedAt != *approved.ReviewedAt || recovered.ReviewedBy == nil || *recovered.ReviewedBy != 1 {
		t.Fatal("approval audit lost in restore")
	}
	if found, err := membership.New(recoveredStore.DB).LookupCard(ctx, recovered.CardCode); err != nil || found.ID != registration.ID {
		t.Fatalf("restored card lookup: %+v %v", found, err)
	}
	var restoredHash string
	if err = recoveredStore.DB.QueryRow("SELECT receipt_token_hash FROM member_registrations WHERE id=?", registration.ID).Scan(&restoredHash); err != nil || restoredHash != receiptHash {
		t.Fatalf("receipt hash changed: %v", err)
	}
	if err = Verify(ctx, archive); err != nil {
		t.Fatalf("restore altered original backup: %v", err)
	}
}
