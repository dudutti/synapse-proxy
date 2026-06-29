package dashboard

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dashboard-static
var staticAssets embed.FS

func StartDashboardServer(port string, listModelsHandler http.HandlerFunc) {
	subFS, err := fs.Sub(staticAssets, "dashboard-static")
	if err != nil {
		panic(err)
	}

	fileServer := http.FileServer(http.FS(subFS))

	// Register /api/models on the Dashboard server DefaultServeMux
	http.HandleFunc("/api/models", listModelsHandler)

	// Global HTTP handler on DefaultServeMux
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Bypass API routes from frontend serving
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/v1/") {
			if r.URL.Path == "/api/models" {
				listModelsHandler(w, r)
			}
			return
		}

		filePath := r.URL.Path
		if filePath == "/" {
			filePath = "/index.html"
		}

		// Check if file exists in embed FS
		_, err := subFS.Open(strings.TrimPrefix(filePath, "/"))
		if err != nil {
			// If not found, try adding .html (e.g. /plans -> /plans.html)
			htmlPath := filePath + ".html"
			if _, err := subFS.Open(strings.TrimPrefix(htmlPath, "/")); err == nil {
				r.URL.Path = htmlPath
			} else {
				// Fallback to index.html for SPA routing
				r.URL.Path = "/index.html"
			}
		}

		fileServer.ServeHTTP(w, r)
	})

	go func() {
		_ = http.ListenAndServe(":"+port, nil)
	}()
}
