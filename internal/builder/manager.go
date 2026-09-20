package builder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
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

var ErrQueueFull = errors.New("build queue is full")
var ErrShuttingDown = errors.New("build service is shutting down")

type Manager struct {
	cfg          *config.Config
	mu           sync.RWMutex
	jobs         map[string]*model.BuildJob
	broadcasters map[string]*LogBroadcaster
	workspaces   map[string]*Workspace
	sem          chan struct{}
	cancelFuncs  map[string]context.CancelFunc
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	closed       bool
}

func NewManager(cfg *config.Config) *Manager {
	c := *cfg
	if c.MaxConcurrentJobs <= 0 {
		c.MaxConcurrentJobs = 2
	}
	if c.MaxQueuedJobs <= 0 {
		c.MaxQueuedJobs = config.DefaultMaxQueuedJobs
	}
	if c.MaxRetainedBuilds <= 0 {
		c.MaxRetainedBuilds = config.DefaultMaxRetainedBuilds
	}
	if c.JobTimeout <= 0 {
		c.JobTimeout = 20 * time.Minute
	}
	if c.MaxDownloadBytes <= 0 {
		c.MaxDownloadBytes = config.DefaultMaxDownloadBytes
	}
	if c.MaxCacheBytes <= 0 {
		c.MaxCacheBytes = config.DefaultMaxCacheBytes
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{
		cfg:          &c,
		jobs:         make(map[string]*model.BuildJob),
		broadcasters: make(map[string]*LogBroadcaster),
		workspaces:   make(map[string]*Workspace),
		sem:          make(chan struct{}, c.MaxConcurrentJobs),
		cancelFuncs:  make(map[string]context.CancelFunc),
		ctx:          ctx,
		cancel:       cancel,
	}
	m.loadHistoricalJobs()
	if _, err := m.PruneOldBuilds(c.MaxRetainedBuilds); err != nil {
		log.Printf("Build retention: %v", err)
	}
	return m
}

func (m *Manager) loadHistoricalJobs() {
	entries, err := os.ReadDir(m.cfg.BuildDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || !validBuildID(e.Name()) {
			continue
		}
		metaPath := filepath.Join(m.cfg.BuildDir, e.Name(), "meta.json")
		info, err := os.Stat(metaPath)
		if err != nil || info.Size() > 1<<20 {
			continue
		}
		data, err := os.ReadFile(metaPath)
		if err != nil {
			continue
		}
		var job model.BuildJob
		if json.Unmarshal(data, &job) != nil || job.BuildID != e.Name() {
			continue
		}
		job.LogFilePath = filepath.Join(m.cfg.BuildDir, e.Name(), "logs", "build.log")
		if !model.IsTerminal(job.Status) {
			now := time.Now()
			job.Status = model.StatusFailed
			job.ErrorMessage = "服务中断或重启，任务未完成"
			job.CurrentStep = "任务已中断"
			job.EndTime = &now
			job.DurationSeconds = now.Sub(job.StartTime).Seconds()
			if err := saveMetadata(&job, metaPath); err != nil {
				log.Printf("Restore build %s: %v", e.Name(), err)
			}
		}
		m.jobs[job.BuildID] = &job
	}
}

func (m *Manager) CreateJob(req *model.CreateBuildRequest) (*model.BuildJob, error) {
	if req == nil {
		return nil, fmt.Errorf("empty build request")
	}
	if req.TargetOS != "" && req.TargetOS != runtime.GOOS || req.TargetArch != "" && req.TargetArch != runtime.GOARCH {
		return nil, fmt.Errorf("only the build host platform %s/%s is supported", runtime.GOOS, runtime.GOARCH)
	}
	args, warns, conflicts, err := nginx.ValidateAndBuildArgs(req.Options, req.PathOverrides, req.ThirdPartySources, nil, req.AutoResolveConflicts)
	if err != nil {
		return nil, fmt.Errorf("参数校验未通过: %w", err)
	}
	if len(conflicts) > 0 {
		return nil, fmt.Errorf("存在编译参数冲突: %s", strings.Join(conflicts, "; "))
	}
	verInfo, err := nginx.GetVersionContext(m.ctx, req.Version)
	if err != nil {
		return nil, fmt.Errorf("解析 Nginx 版本失败: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, ErrShuttingDown
	}
	if len(m.cancelFuncs) >= m.cfg.MaxConcurrentJobs+m.cfg.MaxQueuedJobs {
		return nil, ErrQueueFull
	}
	id := GenerateBuildID()
	ws, err := NewWorkspace(m.cfg.BuildDir, id)
	if err != nil {
		return nil, err
	}
	broadcaster, err := NewLogBroadcaster(ws.BuildLogPath)
	if err != nil {
		_ = os.RemoveAll(ws.BaseDir)
		return nil, err
	}
	job := (&model.BuildJob{
		BuildID:            id,
		NginxVersion:       verInfo.Version,
		TargetOS:           runtime.GOOS,
		TargetArch:         runtime.GOARCH,
		Options:            req.Options,
		PathOverrides:      req.PathOverrides,
		ThirdPartySources:  req.ThirdPartySources,
		ConfigureArguments: args,
		FullConfigureCmd:   "./configure \\\n  " + joinArgs(args),
		Status:             model.StatusQueued,
		CurrentStep:        "已入队，等待 Worker 调度",
		StartTime:          time.Now(),
		CompilerVersion:    runtime.Version(),
		SourceURL:          verInfo.SourceURL,
		HostInfo:           CollectHostInfo(),
		LogFilePath:        ws.BuildLogPath,
	}).Clone()
	if err := saveMetadata(job, ws.MetadataPath); err != nil {
		_ = broadcaster.Close()
		_ = os.RemoveAll(ws.BaseDir)
		return nil, err
	}
	ctx, cancel := context.WithTimeout(m.ctx, m.cfg.JobTimeout)
	m.jobs[id] = job
	m.broadcasters[id] = broadcaster
	m.workspaces[id] = ws
	// Register cancellation and take the response snapshot before exposing the job.
	m.cancelFuncs[id] = cancel
	snapshot := job.Clone()
	m.wg.Add(1)
	go m.runWorker(ctx, job, ws, broadcaster, warns)
	return snapshot, nil
}

func (m *Manager) runWorker(ctx context.Context, job *model.BuildJob, ws *Workspace, broadcaster *LogBroadcaster, warns []string) {
	defer m.wg.Done()
	var buildErr error
	defer func() {
		now := time.Now()
		job.Update(func(j *model.BuildJob) {
			if model.IsTerminal(j.Status) {
				return
			}
			j.EndTime = &now
			j.DurationSeconds = now.Sub(j.StartTime).Seconds()
			switch {
			case errors.Is(ctx.Err(), context.Canceled):
				j.Status = model.StatusCancelled
				j.ErrorMessage = "任务已取消"
				j.CurrentStep = "已取消"
			case ctx.Err() != nil:
				j.Status = model.StatusFailed
				j.ErrorMessage = "任务超时"
				j.CurrentStep = "已超时"
			case buildErr != nil:
				j.Status = model.StatusFailed
				j.ErrorMessage = buildErr.Error()
				j.CurrentStep = "构建失败"
			default:
				j.Status = model.StatusCompleted
				j.Progress = 100
				j.CurrentStep = "编译完成"
			}
		})
		if err := saveMetadata(job, ws.MetadataPath); err != nil {
			log.Printf("Save build %s: %v", job.BuildID, err)
		}
		_ = broadcaster.Close()
		// Keep active registrations until execution, logs and intermediate cleanup have stopped.
		cleanIntermediateFiles(ws)
		m.mu.Lock()
		if cancel := m.cancelFuncs[job.BuildID]; cancel != nil {
			cancel()
		}
		delete(m.cancelFuncs, job.BuildID)
		m.mu.Unlock()
		if _, err := m.PruneOldBuilds(m.cfg.MaxRetainedBuilds); err != nil {
			log.Printf("Build retention: %v", err)
		}
	}()
	select {
	case <-ctx.Done():
		buildErr = ctx.Err()
		return
	case m.sem <- struct{}{}:
	}
	defer func() { <-m.sem }()
	if err := ctx.Err(); err != nil {
		buildErr = err
		return
	}
	for _, warning := range warns {
		_, _ = fmt.Fprintf(broadcaster, "[WARN] %s\n", warning)
	}
	buildErr = ExecuteBuild(ctx, job, ws, m.cfg, broadcaster)
}

func (m *Manager) GetJob(id string) (*model.BuildJob, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	job, ok := m.jobs[id]
	if !ok {
		return nil, false
	}
	return job.Clone(), true
}

func (m *Manager) ListJobs(limit int) []*model.BuildJob {
	m.mu.RLock()
	defer m.mu.RUnlock()
	list := make([]*model.BuildJob, 0, len(m.jobs))
	for _, job := range m.jobs {
		list = append(list, job.Clone())
	}
	sort.Slice(list, func(i, j int) bool { return list[i].StartTime.After(list[j].StartTime) })
	if limit > 0 && len(list) > limit {
		list = list[:limit]
	}
	return list
}

func (m *Manager) SubscribeLogs(id string) (chan string, []string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, ok := m.jobs[id]; !ok {
		return nil, nil, fmt.Errorf("build not found")
	}
	if b := m.broadcasters[id]; b != nil {
		return b.SubscribeWithHistory()
	}
	data, err := readLog(filepath.Join(m.cfg.BuildDir, id, "logs", "build.log"))
	if err != nil && !os.IsNotExist(err) {
		return nil, nil, err
	}
	ch := make(chan string)
	close(ch)
	return ch, []string{string(data)}, nil
}

func (m *Manager) UnsubscribeLogs(id string, ch chan string) {
	m.mu.RLock()
	b := m.broadcasters[id]
	m.mu.RUnlock()
	if b != nil {
		b.Unsubscribe(ch)
	}
}

func (m *Manager) CancelJob(id string) error {
	m.mu.Lock()
	job, ok := m.jobs[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("未找到构建任务: %s", id)
	}
	var cancelErr error
	now := time.Now()
	job.Update(func(j *model.BuildJob) {
		if model.IsTerminal(j.Status) {
			cancelErr = fmt.Errorf("任务当前状态不可取消: %s", j.Status)
			return
		}
		j.Status = model.StatusCancelled
		j.CurrentStep = "已取消"
		j.ErrorMessage = "任务已被用户主动取消"
		j.EndTime = &now
		j.DurationSeconds = now.Sub(j.StartTime).Seconds()
	})
	if cancelErr != nil {
		m.mu.Unlock()
		return cancelErr
	}
	if cancel := m.cancelFuncs[id]; cancel != nil {
		cancel()
	}
	ws := m.workspaces[id]
	// Persist before pruning is able to remove a completed workspace.
	if ws != nil {
		cancelErr = saveMetadata(job, ws.MetadataPath)
	}
	m.mu.Unlock()
	return cancelErr
}

// PruneOldBuilds retains newest terminal jobs and never removes an active worker's files.
func (m *Manager) PruneOldBuilds(maxKeep int) (int, error) {
	if maxKeep <= 0 {
		return 0, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var candidates []*model.BuildJob
	for id, job := range m.jobs {
		if _, active := m.cancelFuncs[id]; active {
			continue
		}
		snapshot := job.Clone()
		if model.IsTerminal(snapshot.Status) {
			candidates = append(candidates, snapshot)
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].StartTime.After(candidates[j].StartTime) })
	if len(candidates) <= maxKeep {
		return 0, nil
	}
	count := 0
	for _, job := range candidates[maxKeep:] {
		if !validBuildID(job.BuildID) {
			continue
		}
		if b := m.broadcasters[job.BuildID]; b != nil {
			_ = b.Close()
		}
		if err := os.RemoveAll(filepath.Join(m.cfg.BuildDir, job.BuildID)); err != nil {
			return count, err
		}
		delete(m.jobs, job.BuildID)
		delete(m.workspaces, job.BuildID)
		delete(m.broadcasters, job.BuildID)
		count++
	}
	return count, nil
}

func (m *Manager) GetArtifactPath(id string) (string, string, error) {
	job, ok := m.GetJob(id)
	if !ok {
		return "", "", fmt.Errorf("未找到构建任务: %s", id)
	}
	if job.Status != model.StatusCompleted || job.Artifact == nil {
		return "", "", fmt.Errorf("构建任务尚未成功完成或产物不存在")
	}
	path, err := safeArtifactPath(filepath.Join(m.cfg.BuildDir, id, "artifacts"), job.Artifact.Name)
	if err != nil {
		return "", "", err
	}
	if _, err := os.Stat(path); err != nil {
		return "", "", err
	}
	return path, job.Artifact.Name, nil
}

func (m *Manager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	m.closed = true
	m.cancel()
	m.mu.Unlock()
	done := make(chan struct{})
	go func() { m.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func joinArgs(args []string) string { return strings.Join(args, " \\\n  ") }
