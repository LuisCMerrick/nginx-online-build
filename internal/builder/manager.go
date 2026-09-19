package builder

import (
	"context"
	"encoding/json"
	"fmt"
	"nginx-builder/internal/config"
	"nginx-builder/internal/model"
	"nginx-builder/internal/nginx"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// Manager coordinates build task scheduling, concurrency throttling, status storage, and log streaming.
type Manager struct {
	cfg          *config.Config
	mu           sync.RWMutex
	jobs         map[string]*model.BuildJob
	broadcasters map[string]*LogBroadcaster
	workspaces   map[string]*Workspace
	sem          chan struct{}
	cancelFuncs  map[string]context.CancelFunc
}

// NewManager initializes the build manager and restores historical jobs from disk.
func NewManager(cfg *config.Config) *Manager {
	m := &Manager{
		cfg:          cfg,
		jobs:         make(map[string]*model.BuildJob),
		broadcasters: make(map[string]*LogBroadcaster),
		workspaces:   make(map[string]*Workspace),
		sem:          make(chan struct{}, cfg.MaxConcurrentJobs),
		cancelFuncs:  make(map[string]context.CancelFunc),
	}

	m.loadHistoricalJobs()
	return m
}

// loadHistoricalJobs scans the builds directory and restores metadata for past builds.
func (m *Manager) loadHistoricalJobs() {
	entries, err := os.ReadDir(m.cfg.BuildDir)
	if err != nil {
		return
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		buildID := e.Name()
		metaPath := filepath.Join(m.cfg.BuildDir, buildID, "meta.json")
		data, err := os.ReadFile(metaPath)
		if err != nil {
			continue
		}
		var job model.BuildJob
		if err := json.Unmarshal(data, &job); err == nil {
			// If a build was interrupted when server crashed/restarted
			if job.Status == model.StatusBuilding || job.Status == model.StatusConfiguring || job.Status == model.StatusDownloading || job.Status == model.StatusQueued {
				job.Status = model.StatusFailed
				job.ErrorMessage = "服务异常中断或重启，任务未完成"
				now := time.Now()
				job.EndTime = &now
			}
			m.jobs[job.BuildID] = &job
		}
	}
}

// CreateJob creates a new build job, validates parameters, and queues it for execution.
func (m *Manager) CreateJob(req *model.CreateBuildRequest) (*model.BuildJob, error) {
	// 1. Resolve Nginx Version
	verInfo, err := nginx.GetVersion(req.Version)
	if err != nil {
		return nil, fmt.Errorf("解析 Nginx 版本失败: %w", err)
	}

	// 2. Validate and build configure arguments
	args, warns, conflicts, err := nginx.ValidateAndBuildArgs(req.Options, req.PathOverrides, req.ThirdPartySources, nil, req.AutoResolveConflicts)
	if err != nil {
		return nil, fmt.Errorf("参数校验未通过: %w", err)
	}
	if len(conflicts) > 0 && !req.AutoResolveConflicts {
		return nil, fmt.Errorf("存在编译参数冲突: %s，请调整选项或启用自动冲突解决", strings.Join(conflicts, "; "))
	}

	targetOS := req.TargetOS
	if targetOS == "" {
		targetOS = runtime.GOOS
	}
	targetArch := req.TargetArch
	if targetArch == "" {
		targetArch = runtime.GOARCH
	}

	buildID := GenerateBuildID()

	// 3. Setup isolated workspace
	ws, err := NewWorkspace(m.cfg.BuildDir, buildID)
	if err != nil {
		return nil, fmt.Errorf("初始化隔离工作区失败: %w", err)
	}

	// 4. Setup log broadcaster
	broadcaster, err := NewLogBroadcaster(ws.BuildLogPath)
	if err != nil {
		return nil, fmt.Errorf("初始化日志通道失败: %w", err)
	}

	fullCmd := fmt.Sprintf("./configure \\\n  %s", "")
	if len(args) > 0 {
		fullCmd = fmt.Sprintf("./configure \\\n  %s", joinArgs(args))
	}

	job := &model.BuildJob{
		BuildID:            buildID,
		NginxVersion:       verInfo.Version,
		TargetOS:           targetOS,
		TargetArch:         targetArch,
		Options:            req.Options,
		PathOverrides:      req.PathOverrides,
		ThirdPartySources:  req.ThirdPartySources,
		ConfigureArguments: args,
		FullConfigureCmd:   fullCmd,
		Status:             model.StatusQueued,
		CurrentStep:        "已入队，等待 Worker 调度",
		Progress:           0,
		StartTime:          time.Now(),
		CompilerVersion:    runtime.Version(),
		SourceURL:          verInfo.SourceURL,
		HostInfo:           CollectHostInfo(),
		LogFilePath:        ws.BuildLogPath,
	}

	// Persist initial state
	saveMetadata(job, ws.MetadataPath)

	m.mu.Lock()
	m.jobs[buildID] = job
	m.broadcasters[buildID] = broadcaster
	m.workspaces[buildID] = ws
	m.mu.Unlock()

	// 5. Spawn background worker
	go m.runWorker(job, ws, broadcaster, warns)

	return job, nil
}

func (m *Manager) runWorker(job *model.BuildJob, ws *Workspace, broadcaster *LogBroadcaster, initialWarns []string) {
	ctx, cancel := context.WithTimeout(context.Background(), m.cfg.JobTimeout)
	m.mu.Lock()
	m.cancelFuncs[job.BuildID] = cancel
	m.mu.Unlock()

	defer func() {
		cancel()
		m.mu.Lock()
		delete(m.cancelFuncs, job.BuildID)
		m.mu.Unlock()
	}()

	// Acquire concurrency slot
	m.sem <- struct{}{}
	defer func() { <-m.sem }()

	// Print queued warnings if any
	for _, w := range initialWarns {
		_, _ = broadcaster.Write([]byte(fmt.Sprintf("[%s] [WARN] %s\n", time.Now().Format("15:04:05"), w)))
	}

	err := ExecuteBuild(ctx, job, ws, m.cfg.CacheDir, broadcaster)

	m.mu.Lock()
	if err != nil {
		now := time.Now()
		job.Update(func(j *model.BuildJob) {
			j.Status = model.StatusFailed
			j.ErrorMessage = err.Error()
			j.EndTime = &now
			j.DurationSeconds = now.Sub(j.StartTime).Seconds()
		})
		saveMetadata(job, ws.MetadataPath)
	}
	m.mu.Unlock()
}

// GetJob returns metadata of a single job.
func (m *Manager) GetJob(buildID string) (*model.BuildJob, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	job, exists := m.jobs[buildID]
	if !exists {
		return nil, false
	}
	return job.Clone(), true
}

// ListJobs returns all jobs sorted descending by start time.
func (m *Manager) ListJobs(limit int) []*model.BuildJob {
	m.mu.RLock()
	defer m.mu.RUnlock()

	list := make([]*model.BuildJob, 0, len(m.jobs))
	for _, j := range m.jobs {
		list = append(list, j.Clone())
	}

	sort.Slice(list, func(i, j int) bool {
		return list[i].StartTime.After(list[j].StartTime)
	})

	if limit > 0 && len(list) > limit {
		list = list[:limit]
	}
	return list
}

// SubscribeLogs returns a channel receiving real-time log lines and historical lines.
func (m *Manager) SubscribeLogs(buildID string) (chan string, []string, error) {
	m.mu.RLock()
	broadcaster, hasBroadcaster := m.broadcasters[buildID]
	ws, hasWs := m.workspaces[buildID]
	m.mu.RUnlock()

	var logPath string
	if hasWs {
		logPath = ws.BuildLogPath
	} else {
		logPath = filepath.Join(m.cfg.BuildDir, buildID, "logs", "build.log")
	}

	var history []string
	if data, err := os.ReadFile(logPath); err == nil {
		history = []string{string(data)}
	}

	if hasBroadcaster {
		return broadcaster.Subscribe(), history, nil
	}

	// Build already finished or broadcaster closed
	ch := make(chan string)
	close(ch)
	return ch, history, nil
}

// UnsubscribeLogs frees listener channel.
func (m *Manager) UnsubscribeLogs(buildID string, ch chan string) {
	m.mu.RLock()
	broadcaster, ok := m.broadcasters[buildID]
	m.mu.RUnlock()

	if ok {
		broadcaster.Unsubscribe(ch)
	}
}

// CancelJob cancels an in-progress or queued build job.
func (m *Manager) CancelJob(buildID string) error {
	m.mu.Lock()
	cancel, ok := m.cancelFuncs[buildID]
	job, exists := m.jobs[buildID]
	m.mu.Unlock()

	if !exists {
		return fmt.Errorf("未找到构建任务: %s", buildID)
	}

	if !ok || job.Status == model.StatusCompleted || job.Status == model.StatusFailed {
		return fmt.Errorf("任务当前状态不可取消 (当前状态: %s)", job.Status)
	}

	cancel()
	now := time.Now()
	job.Update(func(j *model.BuildJob) {
		j.Status = model.StatusFailed
		j.ErrorMessage = "任务已被用户主动取消"
		j.EndTime = &now
		j.DurationSeconds = now.Sub(j.StartTime).Seconds()
	})

	m.mu.RLock()
	ws := m.workspaces[buildID]
	broadcaster := m.broadcasters[buildID]
	m.mu.RUnlock()

	if ws != nil {
		saveMetadata(job, ws.MetadataPath)
	}
	if broadcaster != nil {
		_ = broadcaster.Close()
	}

	return nil
}

// PruneOldBuilds cleans up build workspaces when the total count exceeds maxKeep.
func (m *Manager) PruneOldBuilds(maxKeep int) (int, error) {
	if maxKeep <= 0 {
		return 0, nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.jobs) <= maxKeep {
		return 0, nil
	}

	list := make([]*model.BuildJob, 0, len(m.jobs))
	for _, j := range m.jobs {
		list = append(list, j)
	}

	sort.Slice(list, func(i, j int) bool {
		return list[i].StartTime.After(list[j].StartTime)
	})

	pruneCount := 0
	for idx := maxKeep; idx < len(list); idx++ {
		toDelete := list[idx]
		// Do not delete currently active jobs
		if toDelete.Status == model.StatusQueued || toDelete.Status == model.StatusDownloading ||
			toDelete.Status == model.StatusConfiguring || toDelete.Status == model.StatusBuilding ||
			toDelete.Status == model.StatusPackaging {
			continue
		}

		delete(m.jobs, toDelete.BuildID)
		delete(m.broadcasters, toDelete.BuildID)
		delete(m.workspaces, toDelete.BuildID)
		dir := filepath.Join(m.cfg.BuildDir, toDelete.BuildID)
		_ = os.RemoveAll(dir)
		pruneCount++
	}

	return pruneCount, nil
}

// GetArtifactPath returns the absolute path to the generated tar.gz artifact.
func (m *Manager) GetArtifactPath(buildID string) (string, string, error) {
	job, exists := m.GetJob(buildID)
	if !exists {
		return "", "", fmt.Errorf("未找到构建任务: %s", buildID)
	}

	if job.Status != model.StatusCompleted || job.Artifact == nil {
		return "", "", fmt.Errorf("构建任务尚未成功完成或产物不存在")
	}

	path := filepath.Join(m.cfg.BuildDir, buildID, "artifacts", job.Artifact.Name)
	if _, err := os.Stat(path); err != nil {
		return "", "", fmt.Errorf("产物文件缺失: %w", err)
	}

	return path, job.Artifact.Name, nil
}

func joinArgs(args []string) string {
	var sb strings.Builder
	for i, a := range args {
		sb.WriteString(a)
		if i < len(args)-1 {
			sb.WriteString(" \\\n  ")
		}
	}
	return sb.String()
}
