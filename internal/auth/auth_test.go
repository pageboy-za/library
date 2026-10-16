package auth

import (
	"context"
	"database/sql"
	"errors"
	_ "modernc.org/sqlite"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testPassword = "correct horse battery staple"

func fixture(t *testing.T) (*Service, context.Context) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "auth.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = EnsureSchema(ctx, tx); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return New(db), ctx
}
func administrator(t *testing.T, s *Service, ctx context.Context) User {
	t.Helper()
	u, err := s.Bootstrap(ctx, "Admin", "First administrator", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
func TestPasswordProtectionAndGenericAuthentication(t *testing.T) {
	s, ctx := fixture(t)
	u := administrator(t, s, ctx)
	if u.Username != "admin" || u.Role != RoleAdministrator || !u.Enabled {
		t.Fatalf("bad administrator: %+v", u)
	}
	var encoded string
	if err := s.db.QueryRow("SELECT password_hash FROM staff_users WHERE id=?", u.ID).Scan(&encoded); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(encoded, "pbkdf2-sha256$600000$") || strings.Contains(encoded, testPassword) {
		t.Fatal("password must use versioned salted PBKDF2")
	}
	another, err := passwordHash(testPassword)
	if err != nil {
		t.Fatal(err)
	}
	if another == encoded {
		t.Fatal("password salt reused")
	}
	for _, username := range []string{"admin", "missing"} {
		if _, err = s.Authenticate(ctx, username, "incorrect password"); !errors.Is(err, ErrCredentials) {
			t.Fatalf("login error differs for %s: %v", username, err)
		}
	}
	if got, err := s.Authenticate(ctx, " ADMIN ", testPassword); err != nil || got.ID != u.ID {
		t.Fatalf("authentication: %+v %v", got, err)
	}
	if _, err = s.Bootstrap(ctx, "second", "Second admin", testPassword); !errors.Is(err, ErrConflict) {
		t.Fatalf("bootstrap repeated: %v", err)
	}
	for _, password := range []string{"short", strings.Repeat("x", 129)} {
		if _, err = s.CreateUser(ctx, "invalid", "Invalid", RoleLibrarian, password); !errors.Is(err, ErrInvalid) {
			t.Fatalf("password accepted: %v", err)
		}
	}
	if _, err = s.CreateUser(ctx, "ADMIN", "Duplicate", RoleLibrarian, testPassword); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate username: %v", err)
	}
}
func TestSessionsHashPersistenceExpiryAndCSRF(t *testing.T) {
	s, ctx := fixture(t)
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	u := administrator(t, s, ctx)
	session, err := s.CreateSession(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	var stored string
	if err = s.db.QueryRow("SELECT token_hash FROM staff_sessions").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == session.Token || len(stored) != 64 || !ValidateCSRF(session.Token, session.CSRFToken) || ValidateCSRF(session.Token, session.Token) || ValidateCSRF("", "") {
		t.Fatal("session/CSRF token protection")
	}
	// An independent service instance uses persisted sessions and derives the same CSRF token.
	second := New(s.db)
	second.now = s.now
	restored, err := second.Session(ctx, session.Token)
	if err != nil || restored.User.ID != u.ID || restored.CSRFToken != session.CSRFToken {
		t.Fatalf("persistent session: %+v %v", restored, err)
	}
	now = now.Add(20 * time.Second)
	if _, err = s.Session(ctx, session.Token); err != nil {
		t.Fatal(err)
	}
	var lastSeen int64
	if err = s.db.QueryRow("SELECT last_seen_at FROM staff_sessions").Scan(&lastSeen); err != nil {
		t.Fatal(err)
	}
	if lastSeen != now.Add(-20*time.Second).Unix() {
		t.Fatal("session wrote within the one-minute touch interval")
	}
	now = now.Add(time.Minute)
	if _, err = s.Session(ctx, session.Token); err != nil {
		t.Fatal(err)
	}
	now = now.Add(SessionIdleTimeout)
	if _, err = s.Session(ctx, session.Token); !errors.Is(err, ErrSession) {
		t.Fatalf("idle expiry: %v", err)
	}
	session, err = s.CreateSession(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(SessionLifetime)
	if _, err = s.db.Exec("UPDATE staff_sessions SET last_seen_at=?", now.Unix()); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Session(ctx, session.Token); !errors.Is(err, ErrSession) {
		t.Fatalf("absolute expiry: %v", err)
	}
}
func TestLastAdministratorAndAtomicAccountChanges(t *testing.T) {
	s, ctx := fixture(t)
	u := administrator(t, s, ctx)
	if _, err := s.UpdateUser(ctx, u.ID, u.DisplayName, RoleLibrarian, true, ""); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("last admin demoted: %v", err)
	}
	if _, err := s.UpdateUser(ctx, u.ID, u.DisplayName, RoleAdministrator, false, ""); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("last admin disabled: %v", err)
	}
	other, err := s.CreateUser(ctx, "second", "Second administrator", RoleAdministrator, testPassword)
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.CreateSession(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.UpdateUser(ctx, u.ID, "Changed", RoleLibrarian, true, "short"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid password update: %v", err)
	}
	if restored, err := s.Session(ctx, session.Token); err != nil || restored.User.Role != RoleAdministrator || restored.User.DisplayName != u.DisplayName {
		t.Fatalf("failed update partially applied: %+v %v", restored, err)
	}
	if _, err = s.UpdateUser(ctx, u.ID, u.DisplayName, RoleLibrarian, true, ""); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Session(ctx, session.Token); !errors.Is(err, ErrSession) {
		t.Fatalf("role change retained session: %v", err)
	}
	if _, err = s.UpdateUser(ctx, other.ID, other.DisplayName, RoleAdministrator, false, ""); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("remaining admin disabled: %v", err)
	}
	session, err = s.CreateSession(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.UpdateUser(ctx, u.ID, u.DisplayName, RoleLibrarian, false, ""); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Session(ctx, session.Token); !errors.Is(err, ErrSession) {
		t.Fatalf("disabled account session: %v", err)
	}
	if _, err = s.Authenticate(ctx, u.Username, testPassword); !errors.Is(err, ErrCredentials) {
		t.Fatalf("disabled login: %v", err)
	}
}
func TestPasswordChangesRevokeAllSessions(t *testing.T) {
	s, ctx := fixture(t)
	u := administrator(t, s, ctx)
	one, err := s.CreateSession(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	two, err := s.CreateSession(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ChangePassword(ctx, u.ID, "wrong current password", "another long password"); !errors.Is(err, ErrCredentials) {
		t.Fatalf("wrong current password: %v", err)
	}
	if _, err = s.Session(ctx, one.Token); err != nil {
		t.Fatalf("failed change revoked session: %v", err)
	}
	if err = s.ChangePassword(ctx, u.ID, testPassword, "another long password"); err != nil {
		t.Fatal(err)
	}
	for _, session := range []Session{one, two} {
		if _, err = s.Session(ctx, session.Token); !errors.Is(err, ErrSession) {
			t.Fatalf("password change retained session: %v", err)
		}
	}
	if _, err = s.Authenticate(ctx, u.Username, testPassword); !errors.Is(err, ErrCredentials) {
		t.Fatalf("old password accepted: %v", err)
	}
	if _, err = s.Authenticate(ctx, u.Username, "another long password"); err != nil {
		t.Fatal(err)
	}
	one, err = s.CreateSession(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ResetPassword(ctx, u.ID, testPassword); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Session(ctx, one.Token); !errors.Is(err, ErrSession) {
		t.Fatalf("reset retained session: %v", err)
	}
}
func TestSchemaCreationIsTransactional(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "schema.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = EnsureSchema(ctx, tx); err != nil {
		t.Fatal(err)
	}
	tx.Rollback()
	var n int
	if err = db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name='staff_users'").Scan(&n); err != nil || n != 0 {
		t.Fatalf("schema escaped migration transaction: %d %v", n, err)
	}
}

func TestConcurrentPasswordResetRejectsPreviouslyVerifiedCredential(t *testing.T) {
	s, ctx := fixture(t)
	u := administrator(t, s, ctx)
	_, oldHash, err := s.authenticate(ctx, u.Username, testPassword)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ResetPassword(ctx, u.ID, "new credential after reset"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.createSession(ctx, u.ID, oldHash); !errors.Is(err, ErrCredentials) {
		t.Fatalf("old verified credential created a session after reset: %v", err)
	}
	session, err := s.AuthenticateSession(ctx, u.Username, "new credential after reset")
	if err != nil {
		t.Fatal(err)
	}
	if restored, err := s.Session(ctx, session.Token); err != nil || restored.User.ID != u.ID {
		t.Fatalf("atomic login: %+v %v", restored, err)
	}
}
