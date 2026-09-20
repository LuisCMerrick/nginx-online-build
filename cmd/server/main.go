package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"nginx-builder/internal/auth"
	"nginx-builder/internal/builder"
	"nginx-builder/internal/config"
	"nginx-builder/internal/handler"
	"nginx-builder/web"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
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
	if cfg.BasePath != "" {
		log.Printf("URL base path prefix:  %s", cfg.BasePath)
	}
	log.Printf("Target bind address:   http://%s%s", addr, cfg.BasePath)

	if cfg.AuthEnabled {
		log.Printf("Authentication enabled; open the entry page to sign in")
		if cfg.AuthKeyGenerated {
			log.Printf("Generated access key (this process only): %s", cfg.AuthKey)
		} else {
			log.Printf("Using configured access key (not printed)")
		}
	} else {
		log.Printf("Authentication disabled (--no-auth)")
	}

	log.Printf("--------------------------------------------------")

	mgr := builder.NewManager(cfg)
	h := handler.NewHandler(mgr)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux, cfg.BasePath)

	// Embedded Static web assets
	fileServer := http.FileServer(http.FS(web.FS))

	// If a custom BasePath is configured (e.g. /nginx or /nginx/), handle subpath mounting
	if cfg.BasePath != "" {
		// Redirect /nginx to /nginx/
		mux.HandleFunc(cfg.BasePath, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == cfg.BasePath {
				target := cfg.BasePath + "/"
				if r.URL.RawQuery != "" {
					target += "?" + r.URL.RawQuery
				}
				http.Redirect(w, r, target, http.StatusMovedPermanently)
				return
			}
			http.NotFound(w, r)
		})

		// Serve static assets under /nginx/
		subFS := http.StripPrefix(cfg.BasePath, fileServer)
		mux.HandleFunc(cfg.BasePath+"/", func(w http.ResponseWriter, r *http.Request) {
			relPath := strings.TrimPrefix(r.URL.Path, cfg.BasePath)
			if len(relPath) >= 5 && relPath[:5] == "/api/" {
				http.NotFound(w, r)
				return
			}
			subFS.ServeHTTP(w, r)
		})
	}

	// Always also handle root / for direct access or when reverse proxy strips the prefix
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// If requesting an unmatched API route, return 404
		if len(r.URL.Path) >= 5 && r.URL.Path[:5] == "/api/" {
			http.NotFound(w, r)
			return
		}
		fileServer.ServeHTTP(w, r)
	})

	log.Printf("✔ Web server is ready and listening on http://%s%s", addr, cfg.BasePath)

	// Wrap root router with URL Access Authentication Middleware
	serverHandler := auth.Middleware(cfg, mux)

	server := &http.Server{Addr: addr, Handler: serverHandler, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	stop, release := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer release()
	serverErr := make(chan error, 1)
	go func() { serverErr <- server.ListenAndServe() }()
	failed := false
	select {
	case err := <-serverErr:
		if err != nil && err != http.ErrServerClosed {
			log.Printf("Server error: %v", err)
			failed = true
		}
	case <-stop.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := mgr.Shutdown(shutdown); err != nil {
		log.Printf("Worker shutdown: %v", err)
	}
	if err := server.Shutdown(shutdown); err != nil {
		log.Printf("HTTP shutdown: %v", err)
		_ = server.Close()
	}
	if failed {
		os.Exit(1)
	}
}
