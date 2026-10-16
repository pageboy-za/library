package membership

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"home-library/internal/auth"
	_ "modernc.org/sqlite"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T) (*Service, context.Context, int64) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "membership.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	if _, err = db.Exec("PRAGMA foreign_keys=ON; CREATE TABLE borrowers(id INTEGER PRIMARY KEY,name TEXT NOT NULL,notes TEXT NOT NULL DEFAULT '',archived_at TEXT)"); err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = auth.EnsureSchema(ctx, tx); err != nil {
		t.Fatal(err)
	}
	if err = EnsureSchema(ctx, tx); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	user, err := auth.New(db).Bootstrap(ctx, "admin", "Administrator", "long enough test password")
	if err != nil {
		t.Fatal(err)
	}
	return New(db), ctx, user.ID
}
func register(t *testing.T, s *Service, ctx context.Context) (Registration, string) {
	t.Helper()
	r, token, err := s.Register(ctx, " Ada Lovelace ", " Ada@Example.org ", "")
	if err != nil {
		t.Fatal(err)
	}
	return r, token
}
func TestRegistrationValidationDuplicateAndPrivateReceipt(t *testing.T) {
	s, ctx, _ := fixture(t)
	r, token := register(t, s, ctx)
	if r.Name != "Ada Lovelace" || r.Email != "ada@example.org" || r.Telephone != "" || r.Status != "pending" || r.BorrowerID != nil || r.CardCode != "" {
		t.Fatalf("registration: %+v", r)
	}
	var hash string
	if err := s.db.QueryRow("SELECT receipt_token_hash FROM member_registrations").Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if len(hash) != 64 || hash == token || hash != receiptHash(token) {
		t.Fatal("receipt token not hashed")
	}
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), hash) || strings.Contains(string(raw), token) {
		t.Fatal("receipt secret leaked in registration model")
	}
	if got, err := New(s.db).Receipt(ctx, token); err != nil || got.ID != r.ID {
		t.Fatalf("persisted receipt: %+v %v", got, err)
	}
	if duplicate, receipt, err := s.Register(ctx, "Another name", "ADA@EXAMPLE.ORG", "+44 1234"); !errors.Is(err, ErrConflict) || receipt != "" || duplicate.ID != 0 {
		t.Fatalf("duplicate revealed existing registration: %+v %q %v", duplicate, receipt, err)
	}
	if _, err = s.Receipt(ctx, "invalid receipt"); !errors.Is(err, ErrReceipt) {
		t.Fatalf("bad receipt: %v", err)
	}
	for _, input := range [][3]string{{"", "a@example.org", ""}, {strings.Repeat("n", 101), "a@example.org", ""}, {"Name", "Name <a@example.org>", ""}, {"Name", "missing-email", ""}, {"Name", "a@example.org", "telephone letters"}, {"Name", "a@example.org", strings.Repeat("1", 41)}} {
		if _, _, err = s.Register(ctx, input[0], input[1], input[2]); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid input accepted: %v %v", input, err)
		}
	}
	now := time.Now().Add(ReceiptLifetime)
	s.now = func() time.Time { return now }
	if _, err = s.Receipt(ctx, token); !errors.Is(err, ErrReceipt) {
		t.Fatalf("expired receipt: %v", err)
	}
}
func TestApprovalCardsAndImmutableReview(t *testing.T) {
	s, ctx, actor := fixture(t)
	r, token := register(t, s, ctx)
	approved, err := s.Review(ctx, r.ID, actor, "approve")
	if err != nil {
		t.Fatal(err)
	}
	if approved.Status != "approved" || approved.BorrowerID == nil || !validCard(approved.CardCode) || approved.ReviewedAt == nil || approved.ReviewedBy == nil || *approved.ReviewedBy != actor {
		t.Fatalf("approval: %+v", approved)
	}
	var name, notes string
	if err = s.db.QueryRow("SELECT name,notes FROM borrowers WHERE id=?", *approved.BorrowerID).Scan(&name, &notes); err != nil || name != r.Name || notes != "" {
		t.Fatalf("borrower/contact separation: %q %q %v", name, notes, err)
	}
	if found, err := s.LookupCard(ctx, approved.CardCode); err != nil || *found.BorrowerID != *approved.BorrowerID {
		t.Fatalf("card lookup: %+v %v", found, err)
	}
	if _, err = s.Review(ctx, r.ID, actor, "approve"); !errors.Is(err, ErrState) {
		t.Fatalf("approval repeated: %v", err)
	}
	var count int
	if err = s.db.QueryRow("SELECT count(*) FROM borrowers").Scan(&count); err != nil || count != 1 {
		t.Fatalf("duplicate borrower: %d %v", count, err)
	}
	second, err := auth.New(s.db).CreateUser(ctx, "librarian", "Librarian", auth.RoleLibrarian, "another long password")
	if err != nil {
		t.Fatal(err)
	}
	suspended, err := s.Review(ctx, r.ID, second.ID, "suspend")
	if err != nil {
		t.Fatal(err)
	}
	if suspended.Status != "suspended" {
		t.Fatal(suspended)
	}
	if _, err = s.LookupCard(ctx, approved.CardCode); !errors.Is(err, ErrNotFound) {
		t.Fatalf("suspended card accepted: %v", err)
	}
	rotated, err := s.Review(ctx, r.ID, second.ID, "replace-card")
	if err != nil {
		t.Fatal(err)
	}
	if rotated.CardCode == approved.CardCode || !validCard(rotated.CardCode) {
		t.Fatal("card did not rotate")
	}
	resumed, err := s.Review(ctx, r.ID, second.ID, "reactivate")
	if err != nil {
		t.Fatal(err)
	}
	if *resumed.ReviewedAt != *approved.ReviewedAt || *resumed.ReviewedBy != actor || *resumed.BorrowerID != *approved.BorrowerID {
		t.Fatal("review audit or borrower changed")
	}
	if _, err = s.LookupCard(ctx, approved.CardCode); !errors.Is(err, ErrNotFound) {
		t.Fatalf("replaced card accepted: %v", err)
	}
	if _, err = s.LookupCard(ctx, rotated.CardCode); err != nil {
		t.Fatal(err)
	}
	if receipt, err := s.Receipt(ctx, token); err != nil || receipt.CardCode != rotated.CardCode {
		t.Fatalf("receipt status: %+v %v", receipt, err)
	}
	if _, err = s.db.Exec("UPDATE borrowers SET archived_at='2026-10-16' WHERE id=?", *approved.BorrowerID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.LookupCard(ctx, rotated.CardCode); !errors.Is(err, ErrNotFound) {
		t.Fatalf("archived borrower card accepted: %v", err)
	}
	if receipt, err := s.Receipt(ctx, token); err != nil || receipt.Status != "suspended" || receipt.CardCode != "" {
		t.Fatalf("archived borrower receipt permits card issuance: %+v %v", receipt, err)
	}
}
func TestRejectedApplicationsAndInvalidActions(t *testing.T) {
	s, ctx, actor := fixture(t)
	r, _ := register(t, s, ctx)
	if _, err := s.Review(ctx, r.ID, actor, "reactivate"); !errors.Is(err, ErrState) {
		t.Fatalf("pending registration resumed: %v", err)
	}
	if _, err := s.Review(ctx, r.ID, actor, "replace-card"); !errors.Is(err, ErrState) {
		t.Fatalf("pending card created: %v", err)
	}
	if _, err := s.Review(ctx, r.ID, 999, "approve"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("non-staff reviewer: %v", err)
	}
	rejected, err := s.Review(ctx, r.ID, actor, "reject")
	if err != nil {
		t.Fatal(err)
	}
	if rejected.Status != "rejected" || rejected.BorrowerID != nil || rejected.CardCode != "" {
		t.Fatal(rejected)
	}
	if _, err = s.Review(ctx, r.ID, actor, "approve"); !errors.Is(err, ErrState) {
		t.Fatalf("rejected registration approved: %v", err)
	}
	pending, err := s.List(ctx, "pending")
	if err != nil || len(pending) != 0 {
		t.Fatalf("pending list: %+v %v", pending, err)
	}
	list, err := s.List(ctx, "rejected")
	if err != nil || len(list) != 1 {
		t.Fatalf("rejected list: %+v %v", list, err)
	}
}
func TestFailedApprovalRollsBackBorrowerCreation(t *testing.T) {
	s, ctx, actor := fixture(t)
	r, _ := register(t, s, ctx)
	if _, err := s.db.Exec(`CREATE TRIGGER fail_approval BEFORE UPDATE ON member_registrations WHEN NEW.status='approved' BEGIN SELECT RAISE(ABORT,'fixture failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Review(ctx, r.ID, actor, "approve"); err == nil {
		t.Fatal("failed update accepted")
	}
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM borrowers").Scan(&count); err != nil || count != 0 {
		t.Fatalf("orphan borrower after failed approval: %d %v", count, err)
	}
	list, err := s.List(ctx, "pending")
	if err != nil || len(list) != 1 || list[0].BorrowerID != nil {
		t.Fatalf("failed approval changed registration: %+v %v", list, err)
	}
}
