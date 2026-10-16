// Package membership manages private member applications and circulation cards.
// A card identifies a borrower; it never grants account access or authentication.
package membership

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const ReceiptLifetime = 30 * 24 * time.Hour

var (
	ErrInvalid       = errors.New("Check the membership details")
	ErrConflict      = errors.New("Unable to submit this registration; please ask library staff for help")
	ErrNotFound      = errors.New("Membership not found")
	ErrReceipt       = errors.New("Registration receipt is unavailable or expired; please ask library staff for help")
	ErrState         = errors.New("Membership changed; refresh before trying again")
	telephonePattern = regexp.MustCompile(`^[0-9+(). /-]*$`)
)

type Registration struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	Email       string  `json:"email"`
	Telephone   string  `json:"telephone"`
	Status      string  `json:"status"`
	CardCode    string  `json:"card_code,omitempty"`
	BorrowerID  *int64  `json:"borrower_id,omitempty"`
	SubmittedAt string  `json:"submitted_at"`
	ReviewedAt  *string `json:"reviewed_at,omitempty"`
	ReviewedBy  *int64  `json:"reviewed_by,omitempty"`
}
type Service struct {
	db  *sql.DB
	now func() time.Time
}

func New(db *sql.DB) *Service { return &Service{db: db, now: time.Now} }
func EnsureSchema(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS member_registrations(
 id INTEGER PRIMARY KEY,name TEXT NOT NULL,email TEXT NOT NULL COLLATE NOCASE UNIQUE,telephone TEXT NOT NULL DEFAULT '',
 status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','approved','rejected','suspended')),
 receipt_token_hash TEXT NOT NULL UNIQUE,receipt_expires_at INTEGER NOT NULL,submitted_at TEXT NOT NULL,
 reviewed_at TEXT,reviewed_by INTEGER REFERENCES staff_users(id),borrower_id INTEGER UNIQUE REFERENCES borrowers(id),card_code TEXT UNIQUE);
 CREATE INDEX IF NOT EXISTS member_registrations_status ON member_registrations(status,id);`)
	return err
}
func normalize(name, email, telephone string) (string, string, string, error) {
	name = strings.TrimSpace(name)
	email = strings.ToLower(strings.TrimSpace(email))
	telephone = strings.TrimSpace(telephone)
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 100 || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return "", "", "", ErrInvalid
	}
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || len(email) > 254 || !strings.Contains(email, "@") || strings.IndexFunc(email, unicode.IsControl) >= 0 {
		return "", "", "", fmt.Errorf("%w: enter an email address without a display name", ErrInvalid)
	}
	if len(telephone) > 40 || !telephonePattern.MatchString(telephone) {
		return "", "", "", fmt.Errorf("%w: telephone is optional; use telephone characters only", ErrInvalid)
	}
	return name, email, telephone, nil
}
func randomCode(size int) (string, error) {
	bytes := make([]byte, size)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}
func receiptHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
func validReceipt(token string) bool {
	bytes, err := base64.RawURLEncoding.DecodeString(token)
	return err == nil && len(bytes) == 32 && len(token) == 43
}
func validCard(code string) bool {
	if !strings.HasPrefix(code, "mbr_") {
		return false
	}
	bytes, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(code, "mbr_"))
	return err == nil && len(bytes) == 24 && len(code) == 36
}

const columns = "id,name,email,telephone,status,COALESCE(card_code,''),borrower_id,submitted_at,reviewed_at,reviewed_by"

func scan(row interface{ Scan(...any) error }) (Registration, error) {
	var r Registration
	var borrower, reviewer sql.NullInt64
	var reviewed sql.NullString
	err := row.Scan(&r.ID, &r.Name, &r.Email, &r.Telephone, &r.Status, &r.CardCode, &borrower, &r.SubmittedAt, &reviewed, &reviewer)
	if borrower.Valid {
		r.BorrowerID = &borrower.Int64
	}
	if reviewer.Valid {
		r.ReviewedBy = &reviewer.Int64
	}
	if reviewed.Valid {
		r.ReviewedAt = &reviewed.String
	}
	return r, err
}
func (s *Service) Register(ctx context.Context, name, email, telephone string) (Registration, string, error) {
	name, email, telephone, err := normalize(name, email, telephone)
	if err != nil {
		return Registration{}, "", err
	}
	token, err := randomCode(32)
	if err != nil {
		return Registration{}, "", err
	}
	now := s.now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Registration{}, "", err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT INTO member_registrations(name,email,telephone,receipt_token_hash,receipt_expires_at,submitted_at) VALUES(?,?,?,?,?,?) ON CONFLICT(email) DO NOTHING`, name, email, telephone, receiptHash(token), now.Add(ReceiptLifetime).Unix(), now.Format(time.RFC3339))
	if err != nil {
		return Registration{}, "", err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return Registration{}, "", err
	}
	if n != 1 {
		return Registration{}, "", ErrConflict
	}
	id, err := result.LastInsertId()
	if err != nil {
		return Registration{}, "", err
	}
	registration, err := scan(tx.QueryRowContext(ctx, "SELECT "+columns+" FROM member_registrations WHERE id=?", id))
	if err != nil {
		return Registration{}, "", err
	}
	if err = tx.Commit(); err != nil {
		return Registration{}, "", err
	}
	return registration, token, nil
}
func (s *Service) Receipt(ctx context.Context, token string) (Registration, error) {
	if !validReceipt(token) {
		return Registration{}, ErrReceipt
	}
	// An archived borrower cannot obtain a fresh card or Wallet pass even when
	// their original registration remains approved in its immutable review record.
	registration, err := scan(s.db.QueryRowContext(ctx, `SELECT m.id,m.name,m.email,m.telephone,
 CASE WHEN b.archived_at IS NOT NULL THEN 'suspended' ELSE m.status END,
 CASE WHEN b.archived_at IS NOT NULL THEN '' ELSE COALESCE(m.card_code,'') END,
 m.borrower_id,m.submitted_at,m.reviewed_at,m.reviewed_by
 FROM member_registrations m LEFT JOIN borrowers b ON b.id=m.borrower_id
 WHERE m.receipt_token_hash=? AND m.receipt_expires_at>?`, receiptHash(token), s.now().Unix()))
	if errors.Is(err, sql.ErrNoRows) {
		return Registration{}, ErrReceipt
	}
	return registration, err
}
func (s *Service) List(ctx context.Context, status string) ([]Registration, error) {
	if status != "" && status != "pending" && status != "approved" && status != "rejected" && status != "suspended" {
		return nil, ErrInvalid
	}
	query := "SELECT " + columns + " FROM member_registrations"
	args := []any{}
	if status != "" {
		query += " WHERE status=?"
		args = append(args, status)
	}
	query += " ORDER BY id DESC"
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	registrations := []Registration{}
	for rows.Next() {
		r, err := scan(rows)
		if err != nil {
			return nil, err
		}
		registrations = append(registrations, r)
	}
	return registrations, rows.Err()
}
func (s *Service) Review(ctx context.Context, id, actorID int64, action string) (Registration, error) {
	if id < 1 || actorID < 1 {
		return Registration{}, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Registration{}, err
	}
	defer tx.Rollback()
	var active bool
	if err = tx.QueryRowContext(ctx, "SELECT enabled FROM staff_users WHERE id=?", actorID).Scan(&active); err != nil || !active {
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return Registration{}, err
		}
		return Registration{}, ErrInvalid
	}
	registration, err := scan(tx.QueryRowContext(ctx, "SELECT "+columns+" FROM member_registrations WHERE id=?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return Registration{}, ErrNotFound
	}
	if err != nil {
		return Registration{}, err
	}
	switch action {
	case "approve":
		if registration.Status != "pending" {
			return Registration{}, ErrState
		}
		result, err := tx.ExecContext(ctx, "INSERT INTO borrowers(name,notes) VALUES(?,'')", registration.Name)
		if err != nil {
			return Registration{}, err
		}
		borrower, err := result.LastInsertId()
		if err != nil {
			return Registration{}, err
		}
		code, err := randomCode(24)
		if err != nil {
			return Registration{}, err
		}
		_, err = tx.ExecContext(ctx, `UPDATE member_registrations SET status='approved',borrower_id=?,card_code=?,reviewed_at=?,reviewed_by=? WHERE id=?`, borrower, "mbr_"+code, s.now().UTC().Format(time.RFC3339), actorID, id)
		if err != nil {
			return Registration{}, err
		}
	case "reject":
		if registration.Status != "pending" {
			return Registration{}, ErrState
		}
		if _, err = tx.ExecContext(ctx, "UPDATE member_registrations SET status='rejected',reviewed_at=?,reviewed_by=? WHERE id=?", s.now().UTC().Format(time.RFC3339), actorID, id); err != nil {
			return Registration{}, err
		}
	case "suspend":
		if registration.Status != "approved" {
			return Registration{}, ErrState
		}
		if _, err = tx.ExecContext(ctx, "UPDATE member_registrations SET status='suspended' WHERE id=?", id); err != nil {
			return Registration{}, err
		}
	case "reactivate":
		if registration.Status != "suspended" {
			return Registration{}, ErrState
		}
		if registration.BorrowerID == nil {
			return Registration{}, ErrState
		}
		var archived sql.NullString
		if err = tx.QueryRowContext(ctx, "SELECT archived_at FROM borrowers WHERE id=?", *registration.BorrowerID).Scan(&archived); err != nil {
			return Registration{}, err
		}
		if archived.Valid {
			return Registration{}, ErrState
		}
		if _, err = tx.ExecContext(ctx, "UPDATE member_registrations SET status='approved' WHERE id=?", id); err != nil {
			return Registration{}, err
		}
	case "replace-card":
		if registration.Status != "approved" && registration.Status != "suspended" {
			return Registration{}, ErrState
		}
		code, err := randomCode(24)
		if err != nil {
			return Registration{}, err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE member_registrations SET card_code=? WHERE id=?", "mbr_"+code, id); err != nil {
			return Registration{}, err
		}
	default:
		return Registration{}, ErrInvalid
	}
	result, err := scan(tx.QueryRowContext(ctx, "SELECT "+columns+" FROM member_registrations WHERE id=?", id))
	if err != nil {
		return Registration{}, err
	}
	if err = tx.Commit(); err != nil {
		return Registration{}, err
	}
	return result, nil
}
func (s *Service) Approve(ctx context.Context, id, actorID int64) (Registration, error) {
	return s.Review(ctx, id, actorID, "approve")
}
func (s *Service) LookupCard(ctx context.Context, code string) (Registration, error) {
	if !validCard(code) {
		return Registration{}, ErrNotFound
	}
	result, err := scan(s.db.QueryRowContext(ctx, "SELECT "+columns+" FROM member_registrations WHERE card_code=? AND status='approved' AND EXISTS(SELECT 1 FROM borrowers b WHERE b.id=borrower_id AND b.archived_at IS NULL)", code))
	if errors.Is(err, sql.ErrNoRows) {
		return Registration{}, ErrNotFound
	}
	return result, err
}
