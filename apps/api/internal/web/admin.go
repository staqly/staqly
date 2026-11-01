package web

import (
	"io/fs"
	"log/slog"
	"net/http"
)

// AdminHandler serves the admin panel's static export (site) and, in process, the admin API under
// /api/v1/admin/ (api sees /v1/admin/...). Nothing else of the API is reachable here.
func AdminHandler(site fs.FS, api http.Handler, log *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/api/v1/admin/", http.StripPrefix("/api", api))
	notFound := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":"not_found","message":"not found"}}`))
	}
	mux.HandleFunc("/api/", notFound)
	mux.HandleFunc("/api", notFound)
	mux.Handle("/", files(site, securityHeaders, nil))
	return mux
}
