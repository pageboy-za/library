package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"home-library/internal/auth"
	"home-library/internal/store"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestStaffBoundaryAndMemberProjection(t *testing.T) {
	db, e := store.Open(filepath.Join(t.TempDir(), "library.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.DB.Close()
	ctx := context.Background()
	a := auth.New(db.DB)
	admin, e := a.Bootstrap(ctx, "owner", "Owner", "fictional-test-password")
	if e != nil {
		t.Fatal(e)
	}
	librarian, e := a.CreateUser(ctx, "helper", "Helper", auth.RoleLibrarian, "fictional-test-password")
	if e != nil {
		t.Fatal(e)
	}
	adminSession, _ := a.CreateSession(ctx, admin.ID)
	staffSession, _ := a.CreateSession(ctx, librarian.ID)
	b, e := db.Save(ctx, store.Input{Title: "Public title", Authors: []string{"Public author"}, RequestKey: "member-projection"})
	if e != nil {
		t.Fatal(e)
	}
	_, e = db.SaveDiscovery(ctx, b.ID, store.Discovery{Description: "Public description", Subjects: []string{"Public subject"}, Tags: []string{"Private tag"}, Notes: "Private notes", Revision: 0})
	if e != nil {
		t.Fatal(e)
	}
	h := (&Server{Store: db, Access: a, SecureCookies: true}).Handler()
	request := func(method, path, body string, session *auth.Session, csrf bool, original string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://localhost:8080"+path, strings.NewReader(body))
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		if session != nil {
			r.AddCookie(&http.Cookie{Name: "library_session", Value: session.Token})
			if csrf {
				r.Header.Set("X-CSRF-Token", session.CSRFToken)
			}
		}
		r.Header.Set("X-Forwarded-Method", original)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	for _, p := range []string{"/api/books", "/api/borrowers", "/api/loans", "/api/preferences", "/api/staff/accounts", "/api/auth/authorize-update", "/api/unknown"} {
		if w := request("GET", p, "", nil, false, ""); w.Code != 401 {
			t.Errorf("anonymous %s: %d", p, w.Code)
		}
	}
	for _, p := range []string{"/api/staff/accounts", "/api/auth/authorize-update", "/api/backup-status"} {
		if w := request("GET", p, "", &staffSession, false, ""); w.Code != 403 {
			t.Errorf("librarian %s: %d", p, w.Code)
		}
	}
	if w := request("GET", "/api/books", "", &staffSession, false, ""); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request("POST", "/api/books", "{}", &staffSession, false, ""); w.Code != 403 {
		t.Fatal("missing csrf", w.Code)
	}
	if w := request("GET", "/api/auth/authorize-update", "", &adminSession, false, "POST"); w.Code != 403 {
		t.Fatal("updater missing csrf", w.Code)
	}
	if w := request("GET", "/api/auth/authorize-update", "", &adminSession, true, "POST"); w.Code != 204 {
		t.Fatal("updater admin", w.Code)
	}
	for _, p := range []string{"/api/member/books", fmt.Sprintf("/api/member/books/%d", b.ID)} {
		w := request("GET", p, "", nil, false, "")
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		for _, secret := range []string{"copies", "revision", "Private tag", "Private notes", "password_hash", "borrower"} {
			if strings.Contains(w.Body.String(), secret) {
				t.Errorf("public response leaked %s: %s", secret, w.Body.String())
			}
		}
	}
	w := request("POST", "/api/auth/login", `{"username":"owner","password":"fictional-test-password"}`, nil, false, "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatal("unsafe cookie", cookies)
	}
	var login struct {
		CSRF string `json:"csrf_token"`
	}
	json.Unmarshal(w.Body.Bytes(), &login)
	v := auth.Session{Token: cookies[0].Value, CSRFToken: login.CSRF}
	w = request("POST", "/api/auth/logout", "{}", &v, true, "")
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w = request("GET", "/api/books", "", &v, false, ""); w.Code != 401 {
		t.Fatal("logout session survived", w.Code)
	}
}
func TestMissingAuthFailsClosed(t *testing.T) {
	db, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.DB.Close()
	h := (&Server{Store: db}).Handler()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "http://localhost:8080/api/books", nil))
	if w.Code != 503 {
		t.Fatal(w.Code)
	}
}
func TestLoginGuardBounds(t *testing.T) {
	var g loginGuard
	for i := 0; i < 6; i++ {
		if !g.begin("same") {
			t.Fatal(i)
		}
		g.finish("same", false)
	}
	if g.begin("same") {
		t.Fatal("unbounded login attempts")
	}
	if !g.begin("other") {
		t.Fatal("independent account blocked")
	}
	g.finish("other", true)
}
