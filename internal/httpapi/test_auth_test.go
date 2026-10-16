package httpapi

import (
	"context"
	"home-library/internal/auth"
	"net/http"
	"testing"
)

// testHandler exercises the production access boundary using a persisted staff
// account and real session. Production contains no test-authentication bypass.
func (s *Server) testHandler(t testing.TB) http.Handler {
	t.Helper()
	if s.Store == nil {
		t.Fatal("authenticated test requires a disposable store")
	}
	if s.Access == nil {
		s.Access = auth.New(s.Store.DB)
	}
	ctx := context.Background()
	const username = "test-administrator"
	const password = "httpapi-test-staff-password"
	has, err := s.Access.HasUsers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !has {
		if _, err = s.Access.Bootstrap(ctx, username, "Test administrator", password); err != nil {
			t.Fatal(err)
		}
	}
	session, err := s.Access.AuthenticateSession(ctx, username, password)
	if err != nil {
		t.Fatal(err)
	}
	handler := s.Handler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := r.Cookie("library_session"); err != nil {
			r.AddCookie(&http.Cookie{Name: "library_session", Value: session.Token})
		}
		if r.Header.Get("X-CSRF-Token") == "" {
			r.Header.Set("X-CSRF-Token", session.CSRFToken)
		}
		handler.ServeHTTP(w, r)
	})
}
