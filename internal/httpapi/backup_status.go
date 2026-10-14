package httpapi

import (
	"home-library/internal/backup"
	"net/http"
)

func (s *Server) registerBackupStatus(mux *http.ServeMux) {
	service := backup.NewHealthService(s.BackupDirectory)
	mux.HandleFunc("GET /api/backup-status", func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, http.StatusOK, service.Status(r.Context()))
	})
}
