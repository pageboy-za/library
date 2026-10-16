// Package auth manages staff identities independently of borrower records.
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	RoleAdministrator  = "administrator"
	RoleLibrarian      = "librarian"
	passwordIterations = 600000
	SessionLifetime    = 8 * time.Hour
	SessionIdleTimeout = 30 * time.Minute
)

var (
	ErrCredentials  = errors.New("Incorrect username or password")
	ErrSession      = errors.New("Session expired; sign in again")
	ErrLastAdmin    = errors.New("Keep at least one enabled administrator")
	ErrInvalid      = errors.New("Invalid account details")
	ErrConflict     = errors.New("Username already exists")
	usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
)

type User struct {
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
	Enabled     bool   `json:"enabled"`
}
type Session struct {
	Token     string    `json:"-"`
	CSRFToken string    `json:"csrf_token"`
	User      User      `json:"user"`
	ExpiresAt time.Time `json:"expires_at"`
}
type Service struct {
	db  *sql.DB
	now func() time.Time
}

func New(db *sql.DB) *Service { return &Service{db: db, now: time.Now} }

// EnsureSchema participates in the application's database migration transaction.
func EnsureSchema(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS staff_users (
 id INTEGER PRIMARY KEY, username TEXT NOT NULL COLLATE NOCASE UNIQUE,
 display_name TEXT NOT NULL, role TEXT NOT NULL CHECK(role IN ('administrator','librarian')),
 enabled INTEGER NOT NULL DEFAULT 1 CHECK(enabled IN (0,1)), password_hash TEXT NOT NULL,
 created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL);
 CREATE TABLE IF NOT EXISTS staff_sessions (
 token_hash TEXT PRIMARY KEY, user_id INTEGER NOT NULL REFERENCES staff_users(id) ON DELETE CASCADE,
 created_at INTEGER NOT NULL, expires_at INTEGER NOT NULL, last_seen_at INTEGER NOT NULL);
 CREATE INDEX IF NOT EXISTS staff_sessions_user ON staff_sessions(user_id);`)
	return err
}
func validPassword(password string) bool {
	n := utf8.RuneCountInString(password)
	return n >= 12 && n <= 128 && utf8.ValidString(password)
}
func identity(username, displayName, role string) (string, string, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	displayName = strings.TrimSpace(displayName)
	if !usernamePattern.MatchString(username) || displayName == "" || utf8.RuneCountInString(displayName) > 100 || (role != RoleAdministrator && role != RoleLibrarian) {
		return "", "", ErrInvalid
	}
	return username, displayName, nil
}
func passwordHash(password string) (string, error) {
	if !validPassword(password) {
		return "", fmt.Errorf("%w: password must contain 12 to 128 characters", ErrInvalid)
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, passwordIterations, 32)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pbkdf2-sha256$600000$%s$%s", base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}
func verifyPassword(encoded, password string) bool {
	// A nonexistent user still performs the same expensive password derivation.
	salt := make([]byte, 16)
	expected := make([]byte, 32)
	valid := false
	parts := strings.Split(encoded, "$")
	if len(parts) == 4 && parts[0] == "pbkdf2-sha256" && parts[1] == "600000" {
		s, e1 := base64.RawStdEncoding.DecodeString(parts[2])
		k, e2 := base64.RawStdEncoding.DecodeString(parts[3])
		if e1 == nil && e2 == nil && len(s) == 16 && len(k) == 32 {
			salt = s
			expected = k
			valid = true
		}
	}
	if !validPassword(password) {
		password = "invalid-password-input"
		valid = false
	}
	actual, err := pbkdf2.Key(sha256.New, password, salt, passwordIterations, 32)
	return err == nil && subtle.ConstantTimeCompare(actual, expected) == 1 && valid
}
func scanUser(row interface{ Scan(...any) error }) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Role, &u.Enabled)
	return u, err
}

const userColumns = "id,username,display_name,role,enabled"

func (s *Service) HasUsers(ctx context.Context) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM staff_users").Scan(&n)
	return n > 0, err
}
func (s *Service) Bootstrap(ctx context.Context, username, displayName, password string) (User, error) {
	username, displayName, err := identity(username, displayName, RoleAdministrator)
	if err != nil {
		return User{}, err
	}
	hash, err := passwordHash(password)
	if err != nil {
		return User{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback()
	var count int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM staff_users").Scan(&count); err != nil {
		return User{}, err
	}
	if count != 0 {
		return User{}, fmt.Errorf("%w: staff accounts already configured", ErrConflict)
	}
	u, err := insertUser(ctx, tx, username, displayName, RoleAdministrator, hash, s.now().Unix())
	if err != nil {
		return User{}, err
	}
	return u, tx.Commit()
}
func insertUser(ctx context.Context, tx *sql.Tx, username, displayName, role, hash string, now int64) (User, error) {
	var count int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM staff_users WHERE username=?", username).Scan(&count); err != nil {
		return User{}, err
	}
	if count > 0 {
		return User{}, ErrConflict
	}
	result, err := tx.ExecContext(ctx, "INSERT INTO staff_users(username,display_name,role,password_hash,created_at,updated_at) VALUES(?,?,?,?,?,?)", username, displayName, role, hash, now, now)
	if err != nil {
		return User{}, err
	}
	id, err := result.LastInsertId()
	return User{ID: id, Username: username, DisplayName: displayName, Role: role, Enabled: true}, err
}
func (s *Service) CreateUser(ctx context.Context, username, displayName, role, password string) (User, error) {
	username, displayName, err := identity(username, displayName, role)
	if err != nil {
		return User{}, err
	}
	hash, err := passwordHash(password)
	if err != nil {
		return User{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback()
	u, err := insertUser(ctx, tx, username, displayName, role, hash, s.now().Unix())
	if err != nil {
		return User{}, err
	}
	return u, tx.Commit()
}
func (s *Service) Authenticate(ctx context.Context, username, password string) (User, error) {
	user, _, err := s.authenticate(ctx, username, password)
	return user, err
}

// AuthenticateSession binds session creation to the credential that was verified.
// A concurrent password reset cannot mint a session from an old password after
// revocation: the credential is checked again inside the insertion transaction.
func (s *Service) AuthenticateSession(ctx context.Context, username, password string) (Session, error) {
	user, hash, err := s.authenticate(ctx, username, password)
	if err != nil {
		return Session{}, err
	}
	return s.createSession(ctx, user.ID, hash)
}
func (s *Service) authenticate(ctx context.Context, username, password string) (User, string, error) {
	var u User
	var hash string
	err := s.db.QueryRowContext(ctx, "SELECT "+userColumns+",password_hash FROM staff_users WHERE username=?", strings.ToLower(strings.TrimSpace(username))).Scan(&u.ID, &u.Username, &u.DisplayName, &u.Role, &u.Enabled, &hash)
	matched := verifyPassword(hash, password)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return User{}, "", err
	}
	if !matched || !u.Enabled {
		return User{}, "", ErrCredentials
	}
	return u, hash, nil
}
func (s *Service) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+userColumns+" FROM staff_users ORDER BY username")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := []User{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}
func (s *Service) UpdateUser(ctx context.Context, id int64, displayName, role string, enabled bool, newPassword string) (User, error) {
	_, displayName, err := identity("placeholder", displayName, role)
	if err != nil {
		return User{}, err
	}
	var hash string
	if newPassword != "" {
		hash, err = passwordHash(newPassword)
		if err != nil {
			return User{}, err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback()
	u, err := scanUser(tx.QueryRowContext(ctx, "SELECT "+userColumns+" FROM staff_users WHERE id=?", id))
	if err != nil {
		return User{}, err
	}
	if u.Enabled && u.Role == RoleAdministrator && (!enabled || role != RoleAdministrator) {
		var n int
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM staff_users WHERE enabled=1 AND role=?", RoleAdministrator).Scan(&n); err != nil {
			return User{}, err
		}
		if n <= 1 {
			return User{}, ErrLastAdmin
		}
	}
	if _, err = tx.ExecContext(ctx, "UPDATE staff_users SET display_name=?,role=?,enabled=?,updated_at=? WHERE id=?", displayName, role, enabled, s.now().Unix(), id); err != nil {
		return User{}, err
	}
	if newPassword != "" {
		if _, err = tx.ExecContext(ctx, "UPDATE staff_users SET password_hash=? WHERE id=?", hash, id); err != nil {
			return User{}, err
		}
	}
	if role != u.Role || enabled != u.Enabled || newPassword != "" {
		if _, err = tx.ExecContext(ctx, "DELETE FROM staff_sessions WHERE user_id=?", id); err != nil {
			return User{}, err
		}
	}
	u.DisplayName = displayName
	u.Role = role
	u.Enabled = enabled
	return u, tx.Commit()
}
func (s *Service) ResetPassword(ctx context.Context, id int64, password string) error {
	return s.setPassword(ctx, id, "", password, false)
}
func (s *Service) ChangePassword(ctx context.Context, id int64, current, password string) error {
	return s.setPassword(ctx, id, current, password, true)
}
func (s *Service) setPassword(ctx context.Context, id int64, current, password string, checkCurrent bool) error {
	hash, err := passwordHash(password)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var old string
	if err = tx.QueryRowContext(ctx, "SELECT password_hash FROM staff_users WHERE id=?", id).Scan(&old); err != nil {
		return err
	}
	if checkCurrent && !verifyPassword(old, current) {
		return ErrCredentials
	}
	if _, err = tx.ExecContext(ctx, "UPDATE staff_users SET password_hash=?,updated_at=? WHERE id=?", hash, s.now().Unix(), id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM staff_sessions WHERE user_id=?", id); err != nil {
		return err
	}
	return tx.Commit()
}
func tokenHash(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}
func csrfToken(token string) string {
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write([]byte("home-library-csrf-v1"))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func ValidateCSRF(token, header string) bool {
	if token == "" || header == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(csrfToken(token)), []byte(header)) == 1
}
func validToken(token string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	return err == nil && len(decoded) == 32 && len(token) == 43
}
func (s *Service) CreateSession(ctx context.Context, userID int64) (Session, error) {
	return s.createSession(ctx, userID, "")
}
func (s *Service) createSession(ctx context.Context, userID int64, expectedHash string) (Session, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Session{}, err
	}
	defer tx.Rollback()
	query := "SELECT " + userColumns + " FROM staff_users WHERE id=? AND enabled=1"
	args := []any{userID}
	if expectedHash != "" {
		query += " AND password_hash=?"
		args = append(args, expectedHash)
	}
	u, err := scanUser(tx.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrCredentials
	}
	if err != nil {
		return Session{}, err
	}
	bytes := make([]byte, 32)
	if _, err = rand.Read(bytes); err != nil {
		return Session{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(bytes)
	now := s.now()
	expires := now.Add(SessionLifetime)
	if _, err = tx.ExecContext(ctx, "INSERT INTO staff_sessions(token_hash,user_id,created_at,expires_at,last_seen_at) VALUES(?,?,?,?,?)", tokenHash(token), userID, now.Unix(), expires.Unix(), now.Unix()); err != nil {
		return Session{}, err
	}
	if err = tx.Commit(); err != nil {
		return Session{}, err
	}
	return Session{Token: token, CSRFToken: csrfToken(token), User: u, ExpiresAt: expires}, nil
}
func (s *Service) Session(ctx context.Context, token string) (Session, error) {
	if !validToken(token) {
		return Session{}, ErrSession
	}
	now := s.now()
	var u User
	var expires, lastSeen int64
	err := s.db.QueryRowContext(ctx, `SELECT u.id,u.username,u.display_name,u.role,u.enabled,s.expires_at,s.last_seen_at FROM staff_sessions s JOIN staff_users u ON u.id=s.user_id WHERE s.token_hash=?`, tokenHash(token)).Scan(&u.ID, &u.Username, &u.DisplayName, &u.Role, &u.Enabled, &expires, &lastSeen)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrSession
	}
	if err != nil {
		return Session{}, err
	}
	if !u.Enabled || now.Unix() >= expires || now.Unix()-lastSeen >= int64(SessionIdleTimeout/time.Second) {
		_ = s.RevokeSession(ctx, token)
		return Session{}, ErrSession
	}
	if now.Unix()-lastSeen >= 60 {
		result, err := s.db.ExecContext(ctx, "UPDATE staff_sessions SET last_seen_at=? WHERE token_hash=? AND expires_at>? AND last_seen_at>?", now.Unix(), tokenHash(token), now.Unix(), now.Add(-SessionIdleTimeout).Unix())
		if err != nil {
			return Session{}, err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return Session{}, err
		}
		if n != 1 {
			return Session{}, ErrSession
		}
	}
	return Session{Token: token, CSRFToken: csrfToken(token), User: u, ExpiresAt: time.Unix(expires, 0)}, nil
}
func (s *Service) RevokeSession(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM staff_sessions WHERE token_hash=?", tokenHash(token))
	return err
}
func (s *Service) RevokeUserSessions(ctx context.Context, userID int64) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM staff_sessions WHERE user_id=?", userID)
	return err
}
