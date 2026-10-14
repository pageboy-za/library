package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestReadOnlyAuthenticatedPreview(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" {
			t.Error("Preview credential forwarded upstream")
		}
		if r.Host == "192.168.4.10:8082" {
			t.Error("LAN Host forwarded to local-only app")
		}
		w.Write([]byte("book page"))
	}))
	defer upstream.Close()
	target, _ := url.Parse(upstream.URL)
	address := "192.168.4.10:8082"
	handler := preview(target, address, "test-secret", time.Now().Add(time.Hour))
	request := func(method, path, cookie string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://"+address+path, strings.NewReader("{}"))
		if cookie != "" {
			r.AddCookie(&http.Cookie{Name: "library_preview", Value: cookie})
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if w := request("GET", "/", ""); w.Code != 403 {
		t.Fatalf("Unauthenticated status %d", w.Code)
	}
	login := request("GET", "/?preview=test-secret", "")
	if login.Code != 303 || len(login.Result().Cookies()) != 1 || !login.Result().Cookies()[0].HttpOnly {
		t.Fatal("Expected private cookie and clean redirect")
	}
	if w := request("GET", "/books/1/", "test-secret"); w.Code != 200 || w.Body.String() != "book page" {
		t.Fatalf("Preview failed: %d %s", w.Code, w.Body.String())
	}
	for _, method := range []string{"POST", "PATCH", "DELETE", "PUT"} {
		if w := request(method, "/api/books", "test-secret"); w.Code != 405 {
			t.Fatalf("Mutation allowed: %s", method)
		}
	}
	if w := request("GET", "/", "wrong-token"); w.Code != 403 {
		t.Fatal("Wrong token accepted")
	}
	r := httptest.NewRequest("GET", "http://evil.example/", nil)
	r.AddCookie(&http.Cookie{Name: "library_preview", Value: "test-secret"})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("Wrong host accepted")
	}
	expired := preview(target, address, "test-secret", time.Now().Add(-time.Hour))
	r.Host = address
	w = httptest.NewRecorder()
	expired.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("Expired preview accepted")
	}
}
