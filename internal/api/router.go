package api

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
)

func NewRouter(handlers *Handlers, webDir string, logger *log.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handlers.Health)
	mux.HandleFunc("POST /api/v1/files", handlers.Upload)
	mux.HandleFunc("GET /api/v1/tasks", handlers.ListTasks)
	mux.HandleFunc("GET /api/v1/tasks/{id}", handlers.GetTask)
	mux.HandleFunc("POST /api/v1/tasks/{id}/retry", handlers.RetryTask)
	mux.HandleFunc("POST /api/v1/scan-callback", handlers.ScanCallback)
	staticDir := filepath.Join(webDir, "static")
	if _, err := os.Stat(staticDir); err != nil {
		staticDir = webDir
	}
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir(staticDir))))
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			writeError(w, http.StatusNotFound, "ERR_NOT_FOUND", "资源不存在")
			return
		}
		http.ServeFile(w, r, filepath.Join(webDir, "index.html"))
	})
	return Recover(logger, Logging(logger, mux))
}
