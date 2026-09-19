package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"nginx-builder/internal/builder"
	"nginx-builder/internal/model"
	"nginx-builder/internal/nginx"
	"nginx-builder/internal/system"
	"os"
	"strings"
	"time"
)

type Handler struct {
	mgr *builder.Manager
}

func NewHandler(mgr *builder.Manager) *Handler {
	return &Handler{mgr: mgr}
}

// RegisterRoutes registers all API routes onto the given http.ServeMux.
// If basePath is provided (e.g. "/nginx"), routes are registered both with and without the prefix.
func (h *Handler) RegisterRoutes(mux *http.ServeMux, basePath string) {
	register := func(pattern string, handlerFunc http.HandlerFunc) {
		mux.HandleFunc(pattern, handlerFunc)
		if basePath != "" {
			mux.HandleFunc(basePath+pattern, handlerFunc)
		}
	}

	register("/api/nginx/versions", h.handleGetVersions)
	register("/api/nginx/options", h.handleGetOptions)
	register("/api/nginx/presets", h.handleGetPresets)
	register("/api/nginx/deps", h.handleGetDeps)
	register("/api/nginx/preview", h.handlePreview)

	register("/api/builds", h.handleBuilds)
	register("/api/builds/", h.handleBuildByID)

	// System environment and build dependencies endpoints
	register("/api/system/status", h.handleSystemStatus)
	register("/api/system/deps/install", h.handleInstallSystemDeps)
	register("/api/system/deps/logs", h.handleSystemDepsLogs)
}

func (h *Handler) handleGetVersions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	versions := nginx.GetVersions()
	writeJSON(w, http.StatusOK, map[string]any{
		"success":  true,
		"versions": versions,
	})
}

func (h *Handler) handleGetOptions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	// Group options by Category
	categories := make(map[model.OptionCategory][]model.NginxOption)
	for _, opt := range nginx.OfficialOptions {
		categories[opt.Category] = append(categories[opt.Category], opt)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":       true,
		"options":       nginx.OfficialOptions,
		"categories":    categories,
		"presets":       nginx.ParameterPresets,
		"path_defaults": nginx.AllowedPathOptions,
		"dep_libraries": nginx.DefaultDepLibraries,
	})
}

func (h *Handler) handleGetPresets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"presets": nginx.ParameterPresets,
	})
}

func (h *Handler) handleGetDeps(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success":       true,
		"dep_libraries": nginx.DefaultDepLibraries,
	})
}

func (h *Handler) handlePreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	var req model.PreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "无效的请求格式"})
		return
	}

	verInfo, err := nginx.GetVersion(req.Version)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": err.Error()})
		return
	}

	// In Preview mode, autoResolveConflicts is false so that conflicts can be collected and shown to the user in the UI.
	args, warns, conflicts, err := nginx.ValidateAndBuildArgs(req.Options, req.PathOverrides, req.ThirdPartySources, nil, false)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": err.Error()})
		return
	}

	fullCmd := "./configure"
	if len(args) > 0 {
		fullCmd = fmt.Sprintf("./configure \\\n  %s", strings.Join(args, " \\\n  "))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"data": model.PreviewResponse{
			NginxVersion:   verInfo.Version,
			ConfigureArgs:  args,
			FullCommand:    fullCmd,
			ValidationWarn: warns,
			Conflicts:      conflicts,
			HasConflicts:   len(conflicts) > 0,
		},
	})
}

func (h *Handler) handleBuilds(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		jobs := h.mgr.ListJobs(50)
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"builds":  jobs,
		})
	case http.MethodPost:
		var req model.CreateBuildRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "请求体 JSON 格式不合法"})
			return
		}

		job, err := h.mgr.CreateJob(&req)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": err.Error()})
			return
		}

		writeJSON(w, http.StatusAccepted, map[string]any{
			"success":       true,
			"build_id":      job.BuildID,
			"status":        job.Status,
			"nginx_version": job.NginxVersion,
			"message":       "构建任务已创建并入队",
		})
	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}

func (h *Handler) handleBuildByID(w http.ResponseWriter, r *http.Request) {
	// Path pattern: /api/builds/{build_id} or /api/builds/{build_id}/logs or /api/builds/{build_id}/artifact
	buildID, action, ok := parseBuildRequestPath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}

	switch action {
	case "":
		h.handleGetBuild(w, r, buildID)
	case "logs":
		h.handleGetLogs(w, r, buildID)
	case "artifact":
		h.handleGetArtifact(w, r, buildID)
	default:
		http.NotFound(w, r)
	}
}

func parseBuildRequestPath(path string) (buildID, action string, ok bool) {
	const buildsPrefix = "/api/builds/"
	index := strings.Index(path, buildsPrefix)
	if index == -1 {
		return "", "", false
	}

	parts := strings.Split(strings.Trim(path[index+len(buildsPrefix):], "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		return "", "", false
	}
	if len(parts) > 1 {
		action = parts[1]
	}
	return parts[0], action, true
}

func (h *Handler) handleGetBuild(w http.ResponseWriter, r *http.Request, buildID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	job, exists := h.mgr.GetJob(buildID)
	if !exists {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "error": "未找到对应的构建任务"})
		return
	}

	// Calculate live duration if still running
	if job.Status != model.StatusCompleted && job.Status != model.StatusFailed {
		job.DurationSeconds = time.Since(job.StartTime).Seconds()
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"build":   job,
	})
}

func (h *Handler) handleGetLogs(w http.ResponseWriter, r *http.Request, buildID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check if SSE requested
	accept := r.Header.Get("Accept")
	isSSE := strings.Contains(accept, "text/event-stream") || r.URL.Query().Get("stream") == "true"

	ch, history, err := h.mgr.SubscribeLogs(buildID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if !isSSE {
		// Return all logs as plain text
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		for _, chunk := range history {
			_, _ = w.Write([]byte(chunk))
		}
		h.mgr.UnsubscribeLogs(buildID, ch)
		return
	}

	// SSE stream
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Send initial history
	for _, chunk := range history {
		lines := strings.Split(chunk, "\n")
		for _, line := range lines {
			if line != "" {
				fmt.Fprintf(w, "data: %s\n\n", line)
			}
		}
	}
	flusher.Flush()

	defer h.mgr.UnsubscribeLogs(buildID, ch)

	// Stream live lines
	notify := r.Context().Done()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-notify:
			return
		case chunk, ok := <-ch:
			if !ok {
				// Log channel closed, send done event
				fmt.Fprintf(w, "event: done\ndata: [EOF]\n\n")
				flusher.Flush()
				return
			}
			lines := strings.Split(chunk, "\n")
			for _, line := range lines {
				if line != "" {
					fmt.Fprintf(w, "data: %s\n\n", line)
				}
			}
			flusher.Flush()
		case <-ticker.C:
			// Keep-alive heartbeat comment
			fmt.Fprintf(w, ": heartbeat\n\n")
			flusher.Flush()
		}
	}
}

func (h *Handler) handleGetArtifact(w http.ResponseWriter, r *http.Request, buildID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	path, filename, err := h.mgr.GetArtifactPath(buildID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "error": err.Error()})
		return
	}

	f, err := os.Open(path)
	if err != nil {
		http.Error(w, "无法读取构建产物文件", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		http.Error(w, "获取产物文件信息失败", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", stat.Size()))
	http.ServeContent(w, r, filename, stat.ModTime(), f)
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func (h *Handler) handleSystemStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	res := system.CheckDependencies()
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"data":    res,
	})
}

func (h *Handler) handleInstallSystemDeps(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	err := system.GlobalInstaller.InstallDependencies()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "依赖安装任务已成功触发并在后台执行",
	})
}

func (h *Handler) handleSystemDepsLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	stream := r.URL.Query().Get("stream") == "true"
	if !stream {
		logs := system.GlobalInstaller.GetRecentLogs()
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"logs":    logs,
		})
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// Send existing history logs first
	recent := system.GlobalInstaller.GetRecentLogs()
	for _, line := range recent {
		if line != "" {
			fmt.Fprintf(w, "data: %s\n\n", line)
		}
	}
	flusher.Flush()

	ch := system.GlobalInstaller.Subscribe()
	defer system.GlobalInstaller.Unsubscribe(ch)

	notify := r.Context().Done()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-notify:
			return
		case line, ok := <-ch:
			if !ok {
				fmt.Fprintf(w, "event: done\ndata: [EOF]\n\n")
				flusher.Flush()
				return
			}
			if line != "" {
				fmt.Fprintf(w, "data: %s\n\n", line)
				flusher.Flush()
			}
		case <-ticker.C:
			fmt.Fprintf(w, ": heartbeat\n\n")
			flusher.Flush()
		}
	}
}
