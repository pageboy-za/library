// library-preview provides temporary, token-protected read-only access on a LAN.
package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"
)

func preview(target *url.URL, address, token string, expires time.Time) http.Handler {
	proxy := httputil.NewSingleHostReverseProxy(target)
	original := proxy.Director
	proxy.Director = func(r *http.Request) {
		original(r)
		r.Host = target.Host
		r.Header.Del("Cookie")
		if r.Header.Get("Origin") != "" {
			r.Header.Set("Origin", target.Scheme+"://"+target.Host)
		}
	}
	proxy.ModifyResponse = func(r *http.Response) error {
		r.Header.Set("Referrer-Policy", "no-referrer")
		r.Header.Set("Cache-Control", "no-store")
		return nil
	}
	valid := func(v string) bool { return subtle.ConstantTimeCompare([]byte(v), []byte(token)) == 1 }
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		if r.Host != address || time.Now().After(expires) {
			http.Error(w, "Preview unavailable or expired", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+address {
			http.Error(w, "Same-origin access required", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "This phone preview is read-only. Make changes on the Mac.", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path == "/" && valid(r.URL.Query().Get("preview")) {
			http.SetCookie(w, &http.Cookie{Name: "library_preview", Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, Expires: expires})
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		cookie, err := r.Cookie("library_preview")
		if err != nil || !valid(cookie.Value) {
			http.Error(w, "Open the private preview link to see the library", http.StatusForbidden)
			return
		}
		proxy.ServeHTTP(w, r)
	})
}

func main() {
	address := flag.String("listen", "", "specific private LAN IP and port, for example 192.168.1.20:8082")
	duration := flag.Duration("duration", 2*time.Hour, "temporary preview lifetime (maximum 48 hours)")
	flag.Parse()
	host, _, err := net.SplitHostPort(*address)
	ip := net.ParseIP(host)
	if err != nil || ip == nil || !ip.IsPrivate() || *duration <= 0 || *duration > 48*time.Hour {
		log.Fatal("Use a specific private LAN IP and a duration between zero and 48 hours")
	}
	bytes := make([]byte, 24)
	if _, err = rand.Read(bytes); err != nil {
		log.Fatal(err)
	}
	token := hex.EncodeToString(bytes)
	expires := time.Now().Add(*duration)
	target, _ := url.Parse("http://127.0.0.1:8080")
	server := &http.Server{Addr: *address, Handler: preview(target, *address, token, expires), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	listener, err := net.Listen("tcp", *address)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Phone preview: http://%s/?preview=%s\nRead-only; expires %s\n", *address, token, expires.Format(time.RFC3339))
	time.AfterFunc(*duration, func() { server.Close() })
	if err = server.Serve(listener); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
