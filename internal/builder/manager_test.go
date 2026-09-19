package builder

import (
	"fmt"
	"nginx-builder/internal/config"
	"nginx-builder/internal/model"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCreateJobHonorsAutoResolveConflictsFalse(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "nginx-builder-mgr-test-*")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{
		DataDir:            tmpDir,
		BuildDir:           filepath.Join(tmpDir, "builds"),
		CacheDir:           filepath.Join(tmpDir, "cache"),
		MaxConcurrentJobs:  2,
	}
	_ = os.MkdirAll(cfg.BuildDir, 0755)
	_ = os.MkdirAll(cfg.CacheDir, 0755)

	mgr := NewManager(cfg)

	// Request with mutual conflict: without_http and http_v2
	// AutoResolveConflicts is false
	req := &model.CreateBuildRequest{
		Version:              "1.26.2",
		Options:              []string{"without_http", "http_v2"},
		AutoResolveConflicts: false,
	}

	_, err = mgr.CreateJob(req)
	if err == nil {
		t.Fatalf("expected CreateJob to fail when conflicts exist and AutoResolveConflicts is false")
	}
	if !strings.Contains(err.Error(), "冲突") && !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("expected error to mention conflict, got: %v", err)
	}
}

func TestConcurrentGetJobAndListJobsRace(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "nginx-builder-race-test-*")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{
		DataDir:           tmpDir,
		BuildDir:          filepath.Join(tmpDir, "builds"),
		CacheDir:          filepath.Join(tmpDir, "cache"),
		MaxConcurrentJobs: 4,
	}
	mgr := NewManager(cfg)

	job := &model.BuildJob{
		BuildID:      "race-test-01",
		NginxVersion: "1.26.2",
		Status:       model.StatusQueued,
		Options:      []string{"http_ssl"},
	}
	mgr.mu.Lock()
	mgr.jobs[job.BuildID] = job
	mgr.mu.Unlock()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			// Mutating fields simulating worker
			job.Update(func(j *model.BuildJob) {
				j.Progress = i % 100
				j.Status = model.StatusBuilding
			})
		}
		close(done)
	}()

	for {
		select {
		case <-done:
			return
		default:
			_ = mgr.ListJobs(10)
			_, _ = mgr.GetJob("race-test-01")
		}
	}
}

func TestCancelJobAndPruneOldBuilds(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "nginx-builder-cancel-test-*")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{
		DataDir:           tmpDir,
		BuildDir:          filepath.Join(tmpDir, "builds"),
		CacheDir:          filepath.Join(tmpDir, "cache"),
		MaxConcurrentJobs: 2,
	}
	mgr := NewManager(cfg)

	// Test 1: Cancel non-existent job
	if err := mgr.CancelJob("non-existent"); err == nil {
		t.Fatal("expected error cancelling non-existent job")
	}

	// Test 2: Cancel active job
	job := &model.BuildJob{
		BuildID:      "job-cancel-01",
		NginxVersion: "1.26.2",
		Status:       model.StatusBuilding,
		StartTime:    time.Now(),
	}
	cancelled := false
	mgr.mu.Lock()
	mgr.jobs[job.BuildID] = job
	mgr.cancelFuncs[job.BuildID] = func() { cancelled = true }
	mgr.mu.Unlock()

	if err := mgr.CancelJob(job.BuildID); err != nil {
		t.Fatalf("unexpected error cancelling job: %v", err)
	}
	if !cancelled {
		t.Fatal("expected cancelFunc to be invoked")
	}

	gotJob, _ := mgr.GetJob(job.BuildID)
	if gotJob.Status != model.StatusFailed || !strings.Contains(gotJob.ErrorMessage, "取消") {
		t.Fatalf("expected job status failed with cancel message, got: %s / %s", gotJob.Status, gotJob.ErrorMessage)
	}

	// Test 3: Prune old builds
	for i := 1; i <= 5; i++ {
		bid := fmt.Sprintf("job-old-%d", i)
		bDir := filepath.Join(cfg.BuildDir, bid)
		_ = os.MkdirAll(bDir, 0755)
		mgr.mu.Lock()
		mgr.jobs[bid] = &model.BuildJob{
			BuildID:      bid,
			NginxVersion: "1.26.2",
			Status:       model.StatusCompleted,
			StartTime:    time.Now().Add(-time.Duration(10-i) * time.Hour),
		}
		mgr.mu.Unlock()
	}

	pruned, err := mgr.PruneOldBuilds(2)
	if err != nil {
		t.Fatalf("PruneOldBuilds failed: %v", err)
	}
	if pruned < 3 {
		t.Fatalf("expected at least 3 builds pruned, got %d", pruned)
	}
}
