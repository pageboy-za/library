package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
)

func memberLendingFixture(t *testing.T) (*Store, Book, string) {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "library.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.DB.Close() })
	ctx := context.Background()
	b, err := s.Save(ctx, Input{Title: "Self-service fixture", RequestKey: "self-book-fixture"})
	if err != nil {
		t.Fatal(err)
	}
	borrower, err := s.AddBorrower(ctx, "Fixture Member", "private fixture notes", "self-borrower-fixture")
	if err != nil {
		t.Fatal(err)
	}
	card := "mbr_0123456789abcdefghijklmnopqrstuv"
	_, err = s.DB.Exec(`INSERT INTO member_registrations(name,email,status,receipt_token_hash,receipt_expires_at,submitted_at,borrower_id,card_code) VALUES('Fixture Member','reader@example.test','approved','self-receipt-fixture',9999999999,CURRENT_TIMESTAMP,?,?)`, borrower, card)
	if err != nil {
		t.Fatal(err)
	}
	return s, b, card
}

func TestMemberCheckoutRetryAndRace(t *testing.T) {
	s, b, card := memberLendingFixture(t)
	ctx := context.Background()
	if _, err := s.AddCopy(ctx, b.ID, nil, "self-extra-copy"); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.MemberCheckout(ctx, b.ID, card, "self-same-request"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	loans, err := s.Loans(ctx, b.ID)
	if err != nil || len(loans) != 1 {
		t.Fatalf("retry created duplicate loans: %+v %v", loans, err)
	}
	// A lost-response retry still resolves after the card is replaced or suspended.
	if _, err = s.DB.Exec("UPDATE member_registrations SET status='suspended',card_code=?", card+"new"); err != nil {
		t.Fatal(err)
	}
	if id, err := s.MemberCheckout(ctx, b.ID, card, "self-same-request"); err != nil || id != loans[0].ID {
		t.Fatalf("retry lost its result: %d %v", id, err)
	}
	if _, err = s.MemberCheckout(ctx, b.ID, card, "self-new-request"); !errors.Is(err, ErrMemberPass) {
		t.Fatal("old card accepted", err)
	}
}

func TestMemberCheckoutRejections(t *testing.T) {
	for _, scenario := range []string{"unknown pass", "suspended", "pending", "replaced", "archived borrower", "archived copy", "deleted book", "already lent"} {
		t.Run(scenario, func(t *testing.T) {
			s, b, card := memberLendingFixture(t)
			ctx := context.Background()
			var err error
			want := ErrMemberPass
			switch scenario {
			case "unknown pass":
				card = "mbr_abcdefghijklmnopqrstuvwxyz012345"
			case "suspended", "pending":
				_, err = s.DB.Exec("UPDATE member_registrations SET status=?", scenario)
			case "replaced":
				_, err = s.DB.Exec("UPDATE member_registrations SET card_code=?", card+"new")
			case "archived borrower":
				_, err = s.DB.Exec("UPDATE borrowers SET archived_at=CURRENT_TIMESTAMP")
			case "archived copy":
				_, err = s.DB.Exec("UPDATE copies SET archived_at=CURRENT_TIMESTAMP")
				want = ErrNoLendableCopy
			case "deleted book":
				_, err = s.DB.Exec("UPDATE books SET deleted_at=CURRENT_TIMESTAMP")
				want = ErrNoLendableCopy
			case "already lent":
				_, err = s.MemberCheckout(ctx, b.ID, card, "self-first-loan")
				want = ErrNoLendableCopy
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.MemberCheckout(ctx, b.ID, card, "self-rejected-request"); !errors.Is(err, want) {
				t.Fatalf("expected %v, got %v", want, err)
			}
		})
	}
}

func TestMemberCheckoutConcurrentLastCopy(t *testing.T) {
	s, b, card := memberLendingFixture(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := s.MemberCheckout(ctx, b.ID, card, fmt.Sprintf("self-competing-%d", i))
			if err != nil && !errors.Is(err, ErrNoLendableCopy) {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	loans, err := s.Loans(ctx, b.ID)
	if err != nil || len(loans) != 1 {
		t.Fatalf("last copy lent more than once: %+v %v", loans, err)
	}
}
