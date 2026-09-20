package nginx

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func sourceClient(body string, length int64, calls *int) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		*calls++
		return &http.Response{StatusCode: 200, ContentLength: length, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
}
func TestDownloadLimitsIncludeChunkedResponses(t *testing.T) {
	for _, size := range []int64{9, -1} {
		calls := 0
		cache := t.TempDir()
		dest := t.TempDir()
		_, _, err := fetchSource(context.Background(), dest, cache, "test.tar.gz", "https://1.1.1.1/source", "", DownloadLimits{MaxBytes: 8, CacheBytes: 16}, nil, sourceClient("123456789", size, &calls))
		if !errors.Is(err, ErrSourceTooLarge) {
			t.Fatalf("length %d: %v", size, err)
		}
		entries, _ := os.ReadDir(cache)
		if len(entries) != 0 {
			t.Fatal("oversize download left cache or temp file")
		}
		entries, _ = os.ReadDir(dest)
		if len(entries) != 0 {
			t.Fatal("oversize download published source")
		}
	}
}
func TestCacheIntegrityAndEviction(t *testing.T) {
	cache := t.TempDir()
	dest := t.TempDir()
	calls := 0
	limits := DownloadLimits{MaxBytes: 8, CacheBytes: 8}
	expected := sha256.Sum256([]byte("good"))
	hash := hex.EncodeToString(expected[:])
	os.WriteFile(filepath.Join(cache, "a.tar.gz"), []byte("evil"), 0600)
	path, got, err := fetchSource(context.Background(), dest, cache, "a.tar.gz", "https://1.1.1.1/source", hash, limits, nil, sourceClient("good", 4, &calls))
	if err != nil || got != hash || calls != 1 {
		t.Fatalf("corrupt cache not replaced: %s %v", got, err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "good" {
		t.Fatal("wrong copied content")
	}
	if _, _, err := fetchSource(context.Background(), dest, cache, "a.tar.gz", "https://1.1.1.1/source", hash, limits, nil, sourceClient("evil", 4, &calls)); err != nil || calls != 1 {
		t.Fatal("valid pinned cache not reused", err)
	}
	os.Chtimes(filepath.Join(cache, "a.tar.gz"), time.Unix(1, 0), time.Unix(1, 0))
	for _, name := range []string{"b.tar.gz", "c.tar.gz"} {
		if _, _, err := fetchSource(context.Background(), dest, cache, name, "https://1.1.1.1/source", "", limits, nil, sourceClient("good", 4, &calls)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(cache, "a.tar.gz")); !os.IsNotExist(err) {
		t.Fatal("oldest cache entry was not evicted")
	}
	entries, _ := os.ReadDir(cache)
	var total int64
	for _, e := range entries {
		info, _ := e.Info()
		total += info.Size()
	}
	if total > limits.CacheBytes {
		t.Fatal("cache over budget")
	}
}

type cancelledBody struct{ ctx context.Context }

func (b cancelledBody) Read([]byte) (int, error) { <-b.ctx.Done(); return 0, b.ctx.Err() }
func (b cancelledBody) Close() error             { return nil }
func TestDownloadCancellationCleansTemporaryFiles(t *testing.T) {
	cache := t.TempDir()
	dest := t.TempDir()
	started := make(chan struct{})
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		close(started)
		return &http.Response{StatusCode: 200, ContentLength: -1, Body: cancelledBody{r.Context()}, Header: make(http.Header)}, nil
	})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, _, err := fetchSource(ctx, dest, cache, "a.tar.gz", "https://1.1.1.1/source", "", DownloadLimits{}, nil, client)
		done <- err
	}()
	<-started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("download did not stop")
	}
	entries, _ := os.ReadDir(cache)
	if len(entries) != 0 {
		t.Fatal("cancelled download left partial cache")
	}
}
func TestSharedDownloadWaiterCancellationIsIndependent(t *testing.T) {
	key := filepath.Join(t.TempDir(), "archive")
	release, err := lockDownload(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := lockDownload(ctx, key); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		unlock, err := lockDownload(context.Background(), key)
		if err == nil {
			unlock()
		}
		done <- err
	}()
	release()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("other waiter poisoned by cancellation")
	}
	downloadLocks.Lock()
	defer downloadLocks.Unlock()
	if _, ok := downloadLocks.entries[key]; ok {
		t.Fatal("download lock leaked")
	}
}
