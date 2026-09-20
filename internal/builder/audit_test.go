package builder

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"nginx-builder/internal/config"
	"nginx-builder/internal/model"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func auditManager(t *testing.T, timeout time.Duration) *Manager {
	t.Helper()
	root, err := config.PrepareDataDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := NewManager(&config.Config{DataDir: root, BuildDir: filepath.Join(root, "builds"), CacheDir: filepath.Join(root, "cache"), MaxConcurrentJobs: 1, MaxQueuedJobs: 1, JobTimeout: timeout})
	// Occupy the worker slot, so no build or network download can start.
	m.sem <- struct{}{}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := m.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	})
	return m
}
func TestRejectPlatformBeforeWorkspaceCreation(t *testing.T) {
	m := auditManager(t, time.Minute)
	for _, target := range []string{"../escape", "a/b", `a\b`, "unsupported"} {
		for _, r := range []*model.CreateBuildRequest{{TargetOS: target}, {TargetArch: target}} {
			if _, err := m.CreateJob(r); err == nil {
				t.Fatalf("accepted target %q", target)
			}
		}
	}
	entries, err := os.ReadDir(m.cfg.BuildDir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("created rejected job workspace: %v %v", entries, err)
	}
	for _, name := range []string{"", ".", "..", "../out.tar.gz", `..\out.tar.gz`, "/out.tar.gz"} {
		if _, err := safeArtifactPath(m.cfg.BuildDir, name); err == nil {
			t.Fatalf("accepted filename %q", name)
		}
	}
}
func TestCreateSnapshotQueueLimitAndImmediateCancel(t *testing.T) {
	m := auditManager(t, time.Minute)
	req := &model.CreateBuildRequest{Version: "1.30.5", Options: []string{"stream"}, PathOverrides: map[string]string{"prefix": "/opt/nginx"}}
	first, err := m.CreateJob(req)
	if err != nil {
		t.Fatal(err)
	}
	req.Options[0] = "http_v3"
	req.PathOverrides["prefix"] = "/modified"
	second, err := m.CreateJob(&model.CreateBuildRequest{Version: "1.30.5"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.CreateJob(&model.CreateBuildRequest{Version: "1.30.5"}); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("queue limit: %v", err)
	}
	ch, _, err := m.SubscribeLogs(first.BuildID)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.CancelJob(first.BuildID); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 1000; i++ {
		if first.Status != model.StatusQueued {
			t.Fatal("response snapshot changed")
		}
		m.ListJobs(10)
		m.PruneOldBuilds(20)
	}
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("unexpected log")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled job left SSE open")
	}
	j, _ := m.GetJob(first.BuildID)
	if j.Status != model.StatusCancelled || j.EndTime == nil || j.Options[0] != "stream" || j.PathOverrides["prefix"] != "/opt/nginx" || j.TargetOS != runtime.GOOS || j.TargetArch != runtime.GOARCH {
		t.Fatalf("invalid final job: %+v", j)
	}
	if err := setBuildPhase(context.Background(), m.jobs[first.BuildID], model.StatusPackaging, "late", 90); err == nil {
		t.Fatal("terminal state overwritten")
	}
	if err := m.CancelJob(second.BuildID); err != nil {
		t.Fatal(err)
	}
}
func TestQueuedTimeoutClosesLogsAndPersistsEndTime(t *testing.T) {
	m := auditManager(t, 25*time.Millisecond)
	snapshot, err := m.CreateJob(&model.CreateBuildRequest{Version: "1.30.5"})
	if err != nil {
		t.Fatal(err)
	}
	ch, _, err := m.SubscribeLogs(snapshot.BuildID)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("timed-out queue left SSE open")
	}
	j, _ := m.GetJob(snapshot.BuildID)
	if j.Status != model.StatusFailed || j.EndTime == nil || snapshot.Status != model.StatusQueued {
		t.Fatalf("timeout state=%s snapshot=%s", j.Status, snapshot.Status)
	}
	data, err := os.ReadFile(m.workspaces[j.BuildID].MetadataPath)
	if err != nil {
		t.Fatal(err)
	}
	var persisted model.BuildJob
	if err := json.Unmarshal(data, &persisted); err != nil || persisted.EndTime == nil {
		t.Fatalf("metadata %s: %v", data, err)
	}
}
func TestRestartRecoversEveryNonterminalState(t *testing.T) {
	root := t.TempDir()
	statuses := []model.BuildStatus{model.StatusQueued, model.StatusDownloading, model.StatusConfiguring, model.StatusBuilding, model.StatusPackaging, model.StatusCompleted, model.StatusFailed, model.StatusCancelled}
	for _, status := range statuses {
		ws, err := NewWorkspace(root, string(status))
		if err != nil {
			t.Fatal(err)
		}
		if err := saveMetadata(&model.BuildJob{BuildID: string(status), Status: status, StartTime: time.Now()}, ws.MetadataPath); err != nil {
			t.Fatal(err)
		}
	}
	for pass := 0; pass < 2; pass++ {
		m := NewManager(&config.Config{BuildDir: root})
		for _, status := range statuses {
			j, ok := m.GetJob(string(status))
			if !ok {
				t.Fatal(status)
			}
			if !model.IsTerminal(status) && (j.Status != model.StatusFailed || j.EndTime == nil) {
				t.Fatalf("not recovered: %+v", j)
			}
			if model.IsTerminal(status) && j.Status != status {
				t.Fatal("terminal state changed")
			}
		}
		m.Shutdown(context.Background())
	}
}
func fakeArtifact(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "artifact.tar.gz")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "sbin/nginx", Mode: 0755, Size: int64(len(script))}); err != nil {
		t.Fatal(err)
	}
	tw.Write([]byte(script))
	tw.Close()
	gz.Close()
	f.Close()
	return path
}
func TestSmokeRejectsNonzeroEvenAfterSyntaxOK(t *testing.T) {
	for _, code := range []string{"0", "1"} {
		artifact := fakeArtifact(t, "#!/bin/sh\nif [ \"$1\" = -v ]; then echo 'nginx version: nginx/1.30.5' >&2; exit 0; fi\necho 'syntax is ok' >&2\nexit "+code+"\n")
		_, err := smokeTestArtifact(context.Background(), artifact, "1.30.5", nil)
		if (err == nil) != (code == "0") {
			t.Fatalf("exit %s: %v", code, err)
		}
	}
	artifact := fakeArtifact(t, "#!/bin/sh\necho 'nginx version: nginx/1.30.50' >&2\n")
	if _, err := smokeTestArtifact(context.Background(), artifact, "1.30.5", nil); err == nil {
		t.Fatal("prefix-only version accepted")
	}
	cfg := generateSmokeTestConfig([]string{"--without-http_proxy_module"})
	if !strings.Contains(cfg, "pid logs/nginx.pid") || strings.Contains(cfg, "proxy_temp_path") {
		t.Fatal(cfg)
	}
}
func TestCancelledArchiveWorkAndCommands(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dir := t.TempDir()
	target := filepath.Join(dir, "artifact.tar.gz")
	if _, err := extractTarGzContext(ctx, "missing.tar.gz", dir); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := packageArtifactsContext(ctx, dir, target, "artifact.tar.gz", &model.BuildJob{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("cancelled packaging left artifact")
	}
	ctx, cancel = context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	cmd := newBuildCommand(ctx, "sh", "-c", "sleep 30 & wait")
	if _, err := boundedCommandOutput(cmd); err == nil || time.Since(started) > 2*time.Second {
		t.Fatalf("process tree did not stop promptly: %v", err)
	}
}
func TestLogSizeLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "build.log")
	b, err := NewLogBroadcaster(path)
	if err != nil {
		t.Fatal(err)
	}
	chunk := []byte(strings.Repeat("x", 1<<20))
	for i := 0; i < 20; i++ {
		if n, err := b.Write(chunk); err != nil || n != len(chunk) {
			t.Fatal(n, err)
		}
	}
	b.Close()
	info, _ := os.Stat(path)
	if info.Size() > config.MaxLogBytes+128 {
		t.Fatal("unbounded log")
	}
}

// Cancel after observable disk activity, rather than only before entering the stage.
func TestArchiveCancellationDuringIO(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	if err := os.MkdirAll(filepath.Join(src, "objs"), 0755); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(src, "conf"), 0755)
	payload, err := os.Create(filepath.Join(src, "objs", "nginx"))
	if err != nil {
		t.Fatal(err)
	}
	if err := payload.Truncate(64 << 20); err != nil {
		t.Fatal(err)
	}
	payload.Close()
	artifact := filepath.Join(root, "nginx.tar.gz")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		tick := time.NewTicker(time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				entries, _ := filepath.Glob(filepath.Join(root, ".artifact-*"))
				if len(entries) > 0 {
					cancel()
					return
				}
			}
		}
	}()
	_, err = packageArtifactsContext(ctx, src, artifact, "nginx.tar.gz", &model.BuildJob{StartTime: time.Now()})
	<-finished
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("packaging did not cancel during IO: %v", err)
	}
	if _, err := os.Stat(artifact); !os.IsNotExist(err) {
		t.Fatal("partial artifact published")
	}
	entries, _ := filepath.Glob(filepath.Join(root, ".artifact-*"))
	if len(entries) > 0 {
		t.Fatal("temporary artifact leaked")
	}

	if _, err := packageArtifacts(src, artifact, "nginx.tar.gz", &model.BuildJob{StartTime: time.Now()}); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(root, "extract")
	ctx, cancel = context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	finished = make(chan struct{})
	go func() {
		defer close(finished)
		tick := time.NewTicker(time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				info, err := os.Stat(filepath.Join(dest, "sbin", "nginx"))
				if err == nil && info.Size() > 0 {
					cancel()
					return
				}
			}
		}
	}()
	_, err = extractTarGzContext(ctx, artifact, dest)
	<-finished
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("extraction did not cancel during IO: %v", err)
	}
}
