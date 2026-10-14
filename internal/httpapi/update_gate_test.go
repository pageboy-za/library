package httpapi

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestMaintenanceRejectsMutationBeforeStoreAccess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "maintenance.json")
	if err := os.WriteFile(path, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	s := &Server{Version: "v0.1.0", MaintenancePath: path}
	request := httptest.NewRequest("POST", "http://127.0.0.1:8080/api/books", nil)
	request.Header.Set("Origin", "http://127.0.0.1:8080")
	response := httptest.NewRecorder()
	s.Handler().ServeHTTP(response, request)
	if response.Code != 503 {
		t.Fatalf("write during maintenance: %d", response.Code)
	}
	if s.ActiveWrites.Load() != 0 {
		t.Fatal("write counter leaked")
	}
	version := httptest.NewRecorder()
	s.Handler().ServeHTTP(version, httptest.NewRequest("GET", "http://127.0.0.1:8080/api/version", nil))
	if version.Code != 200 {
		t.Fatal("version must remain readable while updating")
	}
}
