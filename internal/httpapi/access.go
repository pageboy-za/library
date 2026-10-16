package httpapi

import (
	"context"
	"errors"
	"home-library/internal/auth"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type sessionKey struct{}
type loginAttempt struct {
	count int
	since time.Time
}
type loginGuard struct {
	sync.Mutex
	attempts map[string]loginAttempt
	global   loginAttempt
	running  int
}

func (g *loginGuard) begin(name string) bool {
	g.Lock()
	defer g.Unlock()
	now := time.Now()
	if g.attempts == nil {
		g.attempts = map[string]loginAttempt{}
	}
	for k, v := range g.attempts {
		if now.Sub(v.since) > 15*time.Minute {
			delete(g.attempts, k)
		}
	}
	if now.Sub(g.global.since) > time.Minute {
		g.global = loginAttempt{since: now}
	}
	a := g.attempts[name]
	if a.count >= 6 || g.global.count >= 30 || g.running >= 2 || len(g.attempts) >= 1000 {
		return false
	}
	if a.count == 0 {
		a.since = now
	}
	a.count++
	g.attempts[name] = a
	g.global.count++
	g.running++
	return true
}
func (g *loginGuard) finish(name string, success bool) {
	g.Lock()
	defer g.Unlock()
	g.running--
	if success {
		delete(g.attempts, name)
	}
}
func unsafeMethod(method string) bool {
	return method != "GET" && method != "HEAD" && method != "OPTIONS"
}
func (s *Server) accessRequest(w http.ResponseWriter, r *http.Request) (*http.Request, bool) {
	p := r.URL.Path
	public := p == "/healthz" || p == "/api/version" || p == "/api/auth/session" || p == "/api/auth/login" || (p == "/api/member/registration" && r.Method == http.MethodPost) || (p == "/api/member/card/google" && r.Method == http.MethodPost) || strings.HasPrefix(p, "/api/member/") && !unsafeMethod(r.Method)
	staffPage := strings.HasPrefix(p, "/staff") && p != "/staff/login/" && p != "/staff/login"
	privateAPI := strings.HasPrefix(p, "/api/") && !public
	if !privateAPI && !staffPage && !strings.HasPrefix(p, "/covers/") {
		return r, true
	}
	var session auth.Session
	var err error
	if s.Access != nil {
		if c, e := r.Cookie("library_session"); e == nil {
			session, err = s.Access.Session(r.Context(), c.Value)
		} else {
			err = e
		}
	} else {
		err = errors.New("Staff accounts unavailable")
	}
	if session.User.ID == 0 || err != nil {
		if strings.HasPrefix(p, "/covers/") && s.publicCover(r) {
			return r, true
		}
		if staffPage {
			http.Redirect(w, r, "/staff/login/", http.StatusSeeOther)
		} else {
			status := 401
			if s.Access == nil {
				status = 503
			}
			jsonResponse(w, status, map[string]string{"error": "Staff sign-in required"})
		}
		return r, false
	}
	admin := p == "/api/auth/authorize-update" || strings.HasPrefix(p, "/api/staff/") || strings.HasPrefix(p, "/api/updates") || (strings.HasPrefix(p, "/api/preferences") && unsafeMethod(r.Method)) || strings.HasPrefix(p, "/api/backup") || strings.HasPrefix(p, "/api/subtitles") || p == "/api/metadata/backfill" || strings.HasPrefix(p, "/staff/accounts") || strings.HasPrefix(p, "/staff/settings")
	if admin && session.User.Role != auth.RoleAdministrator {
		jsonResponse(w, 403, map[string]string{"error": "Administrator permission required"})
		return r, false
	}
	method := r.Method
	if p == "/api/auth/authorize-update" && r.Header.Get("X-Forwarded-Method") != "" {
		method = r.Header.Get("X-Forwarded-Method")
	}
	if unsafeMethod(method) && !auth.ValidateCSRF(session.Token, r.Header.Get("X-CSRF-Token")) {
		jsonResponse(w, 403, map[string]string{"error": "Refresh your session before saving"})
		return r, false
	}
	return r.WithContext(context.WithValue(r.Context(), sessionKey{}, session)), true
}
func userView(u auth.User) map[string]any {
	return map[string]any{"id": u.ID, "username": u.Username, "display_name": u.DisplayName, "role": u.Role, "enabled": u.Enabled, "disabled": !u.Enabled}
}
func (s *Server) setSessionCookie(w http.ResponseWriter, session auth.Session) {
	http.SetCookie(w, &http.Cookie{Name: "library_session", Value: session.Token, Path: "/", HttpOnly: true, Secure: s.SecureCookies, SameSite: http.SameSiteStrictMode, Expires: session.ExpiresAt})
}
func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: "library_session", Value: "", Path: "/", HttpOnly: true, Secure: s.SecureCookies, SameSite: http.SameSiteStrictMode, MaxAge: -1})
}
func accessError(w http.ResponseWriter, err error) {
	status := 400
	if errors.Is(err, auth.ErrCredentials) {
		status = 401
	}
	if errors.Is(err, auth.ErrConflict) || errors.Is(err, auth.ErrLastAdmin) {
		status = 409
	}
	jsonResponse(w, status, map[string]string{"error": err.Error()})
}
func (s *Server) registerAccess(m *http.ServeMux) {
	m.HandleFunc("GET /api/auth/session", func(w http.ResponseWriter, r *http.Request) {
		if s.Access == nil {
			jsonResponse(w, 503, map[string]string{"error": "Staff accounts unavailable"})
			return
		}
		has, e := s.Access.HasUsers(r.Context())
		if e != nil {
			accessError(w, e)
			return
		}
		out := map[string]any{"user": nil, "setup_required": !has}
		if c, e := r.Cookie("library_session"); e == nil {
			if v, e := s.Access.Session(r.Context(), c.Value); e == nil {
				out["user"] = userView(v.User)
				out["csrf_token"] = v.CSRFToken
			}
		}
		jsonResponse(w, 200, out)
	})
	m.HandleFunc("POST /api/auth/login", func(w http.ResponseWriter, r *http.Request) {
		if s.Access == nil {
			jsonResponse(w, 503, map[string]string{"error": "Staff accounts unavailable"})
			return
		}
		var in struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if e := decode(w, r, &in); e != nil {
			failure(w, e)
			return
		}
		name := strings.ToLower(strings.TrimSpace(in.Username))
		if len(name) > 64 {
			name = "<invalid>"
		}
		if !s.loginGuard.begin(name) {
			w.Header().Set("Retry-After", "60")
			jsonResponse(w, 429, map[string]string{"error": "Too many sign-in attempts; try again later"})
			return
		}
		v, e := s.Access.AuthenticateSession(r.Context(), in.Username, in.Password)
		s.loginGuard.finish(name, e == nil)
		if e != nil {
			accessError(w, e)
			return
		}
		if c, e := r.Cookie("library_session"); e == nil {
			_ = s.Access.RevokeSession(r.Context(), c.Value)
		}
		s.setSessionCookie(w, v)
		jsonResponse(w, 200, map[string]any{"user": userView(v.User), "csrf_token": v.CSRFToken, "setup_required": false})
	})
	m.HandleFunc("POST /api/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		v := r.Context().Value(sessionKey{}).(auth.Session)
		if e := s.Access.RevokeSession(r.Context(), v.Token); e != nil {
			accessError(w, e)
			return
		}
		s.clearSessionCookie(w)
		jsonResponse(w, 200, map[string]bool{"ok": true})
	})
	m.HandleFunc("POST /api/auth/password", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Current string `json:"current_password"`
			New     string `json:"new_password"`
		}
		if e := decode(w, r, &in); e != nil {
			failure(w, e)
			return
		}
		v := r.Context().Value(sessionKey{}).(auth.Session)
		if e := s.Access.ChangePassword(r.Context(), v.User.ID, in.Current, in.New); e != nil {
			accessError(w, e)
			return
		}
		s.clearSessionCookie(w)
		jsonResponse(w, 200, map[string]bool{"ok": true})
	})
	m.HandleFunc("GET /api/auth/authorize-update", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(204)
	})
	m.HandleFunc("GET /api/staff/accounts", func(w http.ResponseWriter, r *http.Request) {
		users, e := s.Access.ListUsers(r.Context())
		if e != nil {
			accessError(w, e)
			return
		}
		out := []map[string]any{}
		for _, u := range users {
			out = append(out, userView(u))
		}
		jsonResponse(w, 200, map[string]any{"accounts": out})
	})
	m.HandleFunc("POST /api/staff/accounts", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Username    string `json:"username"`
			DisplayName string `json:"display_name"`
			Role        string `json:"role"`
			Password    string `json:"password"`
		}
		if e := decode(w, r, &in); e != nil {
			failure(w, e)
			return
		}
		u, e := s.Access.CreateUser(r.Context(), in.Username, in.DisplayName, in.Role, in.Password)
		if e != nil {
			accessError(w, e)
			return
		}
		jsonResponse(w, 201, userView(u))
	})
	m.HandleFunc("PATCH /api/staff/accounts/{id}", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			DisplayName string `json:"display_name"`
			Role        string `json:"role"`
			Disabled    bool   `json:"disabled"`
			Password    string `json:"password"`
		}
		if e := decode(w, r, &in); e != nil {
			failure(w, e)
			return
		}
		n, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if e != nil {
			failure(w, e)
			return
		}
		u, e := s.Access.UpdateUser(r.Context(), n, in.DisplayName, in.Role, !in.Disabled, in.Password)
		if e != nil {
			accessError(w, e)
			return
		}
		jsonResponse(w, 200, userView(u))
	})
}
