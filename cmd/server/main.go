package main

import (
	"log"
	"net/http"
	"nginx-builder/internal/builder"
	"nginx-builder/internal/config"
	"nginx-builder/internal/handler"
	"nginx-builder/web"
	"os"
)

func main() {
	cfg := config.LoadConfig()
	log.Printf("--------------------------------------------------")
	log.Printf("🚀 Nginx 在线编译工具启动中...")
	log.Printf("数据存储路径: %s", cfg.DataDir)
	log.Printf("编译工作目录: %s", cfg.BuildDir)
	log.Printf("源码缓存目录: %s", cfg.CacheDir)
	log.Printf("最大并发编译任务数: %d", cfg.MaxConcurrentJobs)
	log.Printf("单任务超时时间: %v", cfg.JobTimeout)
	log.Printf("服务监听端口: %s", cfg.Port)
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

	addr := ":" + cfg.Port
	log.Printf("✔ Web 服务已就绪，正在监听 http://0.0.0.0:%s", cfg.Port)
	if err := http.ListenAndServe(addr, mux); err != nil && err != http.ErrServerClosed {
		log.Fatalf("服务退出异常: %v", err)
		os.Exit(1)
	}
}
