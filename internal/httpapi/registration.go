package httpapi

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	qrcode "github.com/skip2/go-qrcode"
	"home-library/internal/auth"
	"home-library/internal/membership"
	"home-library/internal/wallet"
)

const memberCookie = "library_member"
const joinCookie = "library_join"

// Rate limits are intentionally local and bounded: this is a private small-library service.
type registrationLimiter struct {
	sync.Mutex
	since  time.Time
	count  int
	recent map[string]loginAttempt
}

func (g *registrationLimiter) allow(key string) bool {
	return g.allowWithin(key, 5, 10)
}

func (g *registrationLimiter) allowWithin(key string, perBrowser, perMinute int) bool {
	g.Lock()
	defer g.Unlock()
	now := time.Now()
	if now.Sub(g.since) >= time.Minute {
		g.since = now
		g.count = 0
	}
	if g.recent == nil {
		g.recent = make(map[string]loginAttempt)
	}
	for k, v := range g.recent {
		if now.Sub(v.since) >= 10*time.Minute {
			delete(g.recent, k)
		}
	}
	if g.count >= perMinute || len(g.recent) >= 1000 {
		return false
	}
	if t, ok := g.recent[key]; ok && t.count >= perBrowser {
		return false
	}
	g.count++
	attempt := g.recent[key]
	if attempt.count == 0 {
		attempt.since = now
	}
	attempt.count++
	g.recent[key] = attempt
	return true
}

// RegistrationURL accepts only a private deployment's explicit base URL. No URL
// supplied in a request can make the Pi fetch a third-party URL or issue a card.
func RegistrationURL(base string) (string, error) {
	u, e := url.Parse(base)
	if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", errors.New("Public URL must be an HTTPS origin without a path")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1")) {
		return "", errors.New("Public URL requires HTTPS except for loopback previews")
	}
	u.Path = "/join/"
	return u.String(), nil
}
func memberFailure(w http.ResponseWriter, e error) {
	status := 500
	message := "Could not complete the membership request; please ask library staff"
	switch {
	case errors.Is(e, membership.ErrInvalid):
		status = 400
		message = "Check your name, email and optional telephone number"
	case errors.Is(e, membership.ErrConflict):
		status = 409
		message = "Unable to register these details; please ask library staff"
	case errors.Is(e, membership.ErrNotFound):
		status = 404
		message = "Member not found"
	case errors.Is(e, membership.ErrReceipt):
		status = 401
		message = "Open your registration in the browser you used to sign up, or ask staff for help"
	case errors.Is(e, membership.ErrState):
		status = 409
		message = "This membership has changed; refresh before continuing"
	case errors.Is(e, wallet.ErrUnavailable):
		status = 503
		message = "This Wallet option has not been configured by the library administrator"
	}
	jsonResponse(w, status, map[string]string{"error": message})
}
func (s *Server) membershipService() *membership.Service {
	if s.Membership != nil {
		return s.Membership
	}
	return membership.New(s.Store.DB)
}
func (s *Server) memberCSRF(w http.ResponseWriter, r *http.Request) (string, bool) {
	c, e := r.Cookie(joinCookie)
	if e != nil || !validMemberToken(c.Value) || subtle.ConstantTimeCompare([]byte(memberCSRFToken(c.Value)), []byte(r.Header.Get("X-Member-CSRF"))) != 1 {
		jsonResponse(w, 403, map[string]string{"error": "Refresh the registration page before continuing"})
		return "", false
	}
	return c.Value, true
}
func memberCSRFToken(seed string) string {
	m := hmac.New(sha256.New, []byte(seed))
	m.Write([]byte("home-library-member-csrf-v1"))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}
func validMemberToken(v string) bool {
	b, e := base64.RawURLEncoding.DecodeString(v)
	return e == nil && len(b) == 32 && len(v) == 43
}
func randomMemberToken() (string, error) {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func (s *Server) memberResponse(w http.ResponseWriter, r *http.Request, registration *membership.Registration) {
	var seed string
	if c, e := r.Cookie(joinCookie); e == nil && validMemberToken(c.Value) {
		seed = c.Value
	} else {
		var e error
		seed, e = randomMemberToken()
		if e != nil {
			memberFailure(w, e)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: joinCookie, Value: seed, Path: "/", HttpOnly: true, Secure: s.SecureCookies, SameSite: http.SameSiteStrictMode, MaxAge: 86400})
	}
	// Reuse the session-token derivation without persisting a second secret.
	csrf := memberCSRFToken(seed)
	var view any
	if registration != nil {
		v := map[string]any{"name": registration.Name, "status": registration.Status}
		if registration.Status == "approved" {
			v["card_code"] = registration.CardCode
		}
		view = v
	}
	status := wallet.Status{}
	if s.Wallet != nil {
		status = s.Wallet.Status()
	}
	jsonResponse(w, 200, map[string]any{"registration": view, "csrf_token": csrf, "wallet": status})
}
func (s *Server) approvedMember(w http.ResponseWriter, r *http.Request) (membership.Registration, bool) {
	c, e := r.Cookie(memberCookie)
	if e != nil {
		memberFailure(w, membership.ErrReceipt)
		return membership.Registration{}, false
	}
	v, e := s.membershipService().Receipt(r.Context(), c.Value)
	if e != nil {
		memberFailure(w, e)
		return v, false
	}
	if v.Status != "approved" {
		jsonResponse(w, 403, map[string]string{"error": "An active, staff-approved membership is required"})
		return v, false
	}
	return v, true
}
func writeQR(w http.ResponseWriter, contents string) {
	data, e := qrcode.Encode(contents, qrcode.Medium, 320)
	if e != nil {
		memberFailure(w, e)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Write(data)
}
func (s *Server) registerMembership(m *http.ServeMux) {
	m.HandleFunc("GET /api/member/registration", func(w http.ResponseWriter, r *http.Request) {
		var registration *membership.Registration
		if c, e := r.Cookie(memberCookie); e == nil {
			v, e := s.membershipService().Receipt(r.Context(), c.Value)
			if e == nil {
				registration = &v
			} else if !errors.Is(e, membership.ErrReceipt) {
				memberFailure(w, e)
				return
			}
		}
		s.memberResponse(w, r, registration)
	})
	m.HandleFunc("POST /api/member/registration", func(w http.ResponseWriter, r *http.Request) {
		seed, ok := s.memberCSRF(w, r)
		if !ok {
			return
		}
		if !s.registrationLimiter.allow(seed) {
			w.Header().Set("Retry-After", "600")
			jsonResponse(w, 429, map[string]string{"error": "Please wait before registering again, or ask staff for help"})
			return
		}
		if c, e := r.Cookie(memberCookie); e == nil {
			if _, e = s.membershipService().Receipt(r.Context(), c.Value); e == nil {
				jsonResponse(w, 409, map[string]string{"error": "This browser already has a registration; open Your library card"})
				return
			}
		}
		var in struct {
			Name      string `json:"name"`
			Email     string `json:"email"`
			Telephone string `json:"telephone"`
		}
		if e := decode(w, r, &in); e != nil {
			failure(w, e)
			return
		}
		v, token, e := s.membershipService().Register(r.Context(), in.Name, in.Email, in.Telephone)
		if e != nil {
			memberFailure(w, e)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: memberCookie, Value: token, Path: "/", HttpOnly: true, Secure: s.SecureCookies, SameSite: http.SameSiteStrictMode, MaxAge: 30 * 86400})
		s.memberResponse(w, r, &v)
	})
	m.HandleFunc("GET /api/member/card/qr", func(w http.ResponseWriter, r *http.Request) {
		v, ok := s.approvedMember(w, r)
		if !ok {
			return
		}
		writeQR(w, v.CardCode)
	})
	m.HandleFunc("GET /api/member/card/apple", func(w http.ResponseWriter, r *http.Request) {
		v, ok := s.approvedMember(w, r)
		if !ok {
			return
		}
		if s.Wallet == nil {
			memberFailure(w, wallet.ErrUnavailable)
			return
		}
		data, e := s.Wallet.Apple(wallet.Card{Code: v.CardCode, Name: v.Name})
		if e != nil {
			memberFailure(w, e)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.apple.pkpass")
		w.Header().Set("Content-Disposition", `attachment; filename="library-card.pkpass"`)
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Write(data)
	})
	m.HandleFunc("POST /api/member/card/google", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.memberCSRF(w, r); !ok {
			return
		}
		var empty struct{}
		if e := decode(w, r, &empty); e != nil {
			failure(w, e)
			return
		}
		v, ok := s.approvedMember(w, r)
		if !ok {
			return
		}
		if s.Wallet == nil {
			memberFailure(w, wallet.ErrUnavailable)
			return
		}
		link, e := s.Wallet.Google(wallet.Card{Code: v.CardCode, Name: v.Name})
		if e != nil {
			memberFailure(w, e)
			return
		}
		jsonResponse(w, 200, map[string]string{"url": link})
	})
	m.HandleFunc("GET /api/members/config", func(w http.ResponseWriter, r *http.Request) {
		base := s.PublicURL
		if base == "" {
			base = "https://library.local"
		}
		registrationURL, e := RegistrationURL(base)
		if e != nil {
			memberFailure(w, e)
			return
		}
		jsonResponse(w, 200, map[string]string{"registration_url": registrationURL})
	})
	m.HandleFunc("GET /api/members/registration-qr", func(w http.ResponseWriter, r *http.Request) {
		base := s.PublicURL
		if base == "" {
			base = "https://library.local"
		}
		u, e := RegistrationURL(base)
		if e != nil {
			memberFailure(w, e)
			return
		}
		writeQR(w, u)
	})
	m.HandleFunc("GET /api/members", func(w http.ResponseWriter, r *http.Request) {
		status := r.URL.Query().Get("status")
		if status == "all" {
			status = ""
		}
		v, e := s.membershipService().List(r.Context(), status)
		if e != nil {
			memberFailure(w, e)
			return
		}
		jsonResponse(w, 200, map[string]any{"members": v})
	})
	m.HandleFunc("POST /api/members/{id}/review", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Action string `json:"action"`
		}
		if e := decode(w, r, &in); e != nil {
			failure(w, e)
			return
		}
		actor := r.Context().Value(sessionKey{}).(auth.Session)
		v, e := s.membershipService().Review(r.Context(), id(r), actor.User.ID, in.Action)
		if e != nil {
			memberFailure(w, e)
			return
		}
		jsonResponse(w, 200, map[string]any{"member": v})
	})
	m.HandleFunc("POST /api/members/lookup", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Code string `json:"card_code"`
		}
		if e := decode(w, r, &in); e != nil {
			failure(w, e)
			return
		}
		v, e := s.membershipService().LookupCard(r.Context(), strings.TrimSpace(in.Code))
		if e != nil {
			memberFailure(w, e)
			return
		}
		jsonResponse(w, 200, map[string]any{"member": v})
	})
}
