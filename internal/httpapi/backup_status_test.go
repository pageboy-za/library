package httpapi

import (
	"encoding/json"
	"home-library/internal/backup"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBackupStatusEndpointReadOnlyContract(t *testing.T) {
	mux := http.NewServeMux()
	(&Server{}).registerBackupStatus(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/backup-status?directory=/private/secret", nil))
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Header())
	}
	var status backup.Status
	if e := json.Unmarshal(w.Body.Bytes(), &status); e != nil {
		t.Fatal(e)
	}
	if status.Configured || status.State != "unconfigured" || status.CheckedAt == "" {
		t.Fatalf("Contract %+v", status)
	}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/backup-status", nil))
	if w.Code != 405 {
		t.Fatal("Write method exposed")
	}
}
