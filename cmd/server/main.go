package main

import (
	"log"
	"net"
	"net/http"
	"nginx-builder/internal/builder"
	"nginx-builder/internal/config"
	"nginx-builder/internal/handler"
	"nginx-builder/web"
	"os"
)

func main() {
	cfg := config.LoadConfig()

	addr := net.JoinHostPort(cfg.Host, cfg.Port)

	log.Printf("--------------------------------------------------")
	log.Printf("🚀 Starting Nginx Online Web Builder v%s...", config.AppVersion)
	log.Printf("Data storage path:     %s", cfg.DataDir)
	log.Printf("Compilation workspace: %s", cfg.BuildDir)
	log.Printf("Source tarball cache:  %s", cfg.CacheDir)
	log.Printf("Max concurrent jobs:   %d", cfg.MaxConcurrentJobs)
	log.Printf("Job execution timeout: %v", cfg.JobTimeout)
	log.Printf("Target bind address:   http://%s", addr)
	log.Printf("--------------------------------------------------")

	mgr := builder.NewManager(cfg)
	h := handler.NewHandler(mgr)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// Embedded Static web assets
	fileServer := http.FileServer(http.FS(web.FS))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// If requesting an unmatched API route, return 404
		if len(r.URL.Path) >= 5 && r.URL.Path[:5] == "/api/" {
			http.NotFound(w, r)
			return
		}
		fileServer.ServeHTTP(w, r)
	})

	log.Printf("✔ Web server is ready and listening on http://%s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server terminated with error: %v", err)
		os.Exit(1)
	}
}
