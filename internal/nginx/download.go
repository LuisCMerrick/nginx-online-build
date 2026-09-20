package nginx

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"nginx-builder/internal/config"
	"nginx-builder/internal/safenet"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type DownloadLimits struct {
	MaxBytes   int64
	CacheBytes int64
}

var ErrSourceTooLarge = errors.New("source archive exceeds download size limit")
var cacheMu sync.Mutex
var downloadLocks = struct {
	sync.Mutex
	entries map[string]*downloadLock
}{entries: make(map[string]*downloadLock)}

type downloadLock struct {
	token chan struct{}
	refs  int
}

func lockDownload(ctx context.Context, key string) (func(), error) {
	downloadLocks.Lock()
	entry := downloadLocks.entries[key]
	if entry == nil {
		entry = &downloadLock{token: make(chan struct{}, 1)}
		entry.token <- struct{}{}
		downloadLocks.entries[key] = entry
	}
	entry.refs++
	downloadLocks.Unlock()
	releaseRef := func() {
		downloadLocks.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(downloadLocks.entries, key)
		}
		downloadLocks.Unlock()
	}
	select {
	case <-ctx.Done():
		releaseRef()
		return nil, ctx.Err()
	case <-entry.token:
		return func() { entry.token <- struct{}{}; releaseRef() }, nil
	}
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func normalizedLimits(limits DownloadLimits) DownloadLimits {
	if limits.MaxBytes <= 0 {
		limits.MaxBytes = config.DefaultMaxDownloadBytes
	}
	if limits.CacheBytes <= 0 {
		limits.CacheBytes = config.DefaultMaxCacheBytes
	}
	return limits
}

// fetchSource serializes same-file downloads. Cancelling one waiter never cancels another job.
// Cache eviction and copy-out share a lock so an archive cannot disappear during a copy.
func fetchSource(ctx context.Context, destDir, cacheDir, name, sourceURL, expected string, limits DownloadLimits, writer io.Writer, client *http.Client) (string, string, error) {
	limits = normalizedLimits(limits)
	if name == "" || filepath.Base(name) != name || strings.ContainsAny(name, "/\\") {
		return "", "", fmt.Errorf("invalid source filename")
	}
	if expected != "" {
		if value, err := hex.DecodeString(expected); err != nil || len(value) != sha256.Size {
			return "", "", fmt.Errorf("invalid expected SHA256")
		}
	}
	cacheDir, err := filepath.Abs(cacheDir)
	if err != nil {
		return "", "", err
	}
	if err = os.MkdirAll(cacheDir, 0755); err != nil {
		return "", "", err
	}
	cachePath := filepath.Join(cacheDir, name)
	unlock, err := lockDownload(ctx, cachePath)
	if err != nil {
		return "", "", err
	}
	defer unlock()
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	destination := filepath.Join(destDir, name)
	cacheMu.Lock()
	info, statErr := os.Lstat(cachePath)
	if statErr == nil && info.Mode().IsRegular() {
		if info.Size() > limits.MaxBytes {
			cacheMu.Unlock()
			return "", "", ErrSourceTooLarge
		}
		hash, hashErr := fileSHA256Context(ctx, cachePath)
		if hashErr == nil && (expected == "" || hash == expected) {
			err = trimCache(cacheDir, limits.CacheBytes, 0, cachePath)
			if err == nil {
				err = copyFileContext(ctx, cachePath, destination)
			}
			if err == nil {
				_ = os.Chtimes(cachePath, time.Now(), time.Now())
			}
			cacheMu.Unlock()
			if err != nil {
				return "", "", err
			}
			if writer != nil {
				fmt.Fprintf(writer, "[Source] Cache hit: %s (SHA256: %s)\n", name, hash)
			}
			return destination, hash, nil
		}
		if ctx.Err() != nil {
			cacheMu.Unlock()
			return "", "", ctx.Err()
		}
		_ = os.Remove(cachePath)
	} else if statErr == nil {
		cacheMu.Unlock()
		return "", "", fmt.Errorf("cache entry is not a regular file")
	}
	cacheMu.Unlock()
	if err := safenet.ValidateURLHostContext(ctx, sourceURL); err != nil {
		return "", "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return "", "", err
	}
	if writer != nil {
		fmt.Fprintf(writer, "[Source] Downloading %s\n", name)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("source download HTTP %s", resp.Status)
	}
	if resp.ContentLength > limits.MaxBytes {
		return "", "", ErrSourceTooLarge
	}
	temp, err := os.CreateTemp(cacheDir, ".download-*")
	if err != nil {
		return "", "", err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	hasher := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(temp, hasher), io.LimitReader(contextReader{ctx, resp.Body}, limits.MaxBytes+1))
	closeErr := temp.Close()
	if copyErr != nil {
		return "", "", copyErr
	}
	if closeErr != nil {
		return "", "", closeErr
	}
	if written > limits.MaxBytes {
		return "", "", ErrSourceTooLarge
	}
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	hash := hex.EncodeToString(hasher.Sum(nil))
	if expected != "" && expected != hash {
		return "", "", fmt.Errorf("source SHA256 mismatch: expected %s, got %s", expected, hash)
	}
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if err = trimCache(cacheDir, limits.CacheBytes, written, cachePath); err != nil {
		return "", "", err
	}
	if err = os.Rename(tempName, cachePath); err != nil {
		return "", "", err
	}
	if err = copyFileContext(ctx, cachePath, destination); err != nil {
		return "", "", err
	}
	if writer != nil {
		if expected != "" {
			fmt.Fprintf(writer, "[Source] Expected SHA256 verified: %s\n", hash)
		} else {
			fmt.Fprintf(writer, "[Source] SHA256 recorded (no trusted expected hash): %s\n", hash)
		}
	}
	return destination, hash, nil
}

func trimCache(dir string, budget, incoming int64, protected string) error {
	if incoming > budget {
		return fmt.Errorf("source exceeds cache capacity")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	type candidate struct {
		path     string
		size     int64
		modified time.Time
	}
	var list []candidate
	total := incoming
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".tar.gz") || e.Type()&os.ModeSymlink != 0 {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		path := filepath.Join(dir, e.Name())
		if path == protected && incoming > 0 {
			continue
		}
		total += info.Size()
		if path != protected {
			list = append(list, candidate{path, info.Size(), info.ModTime()})
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].modified.Before(list[j].modified) })
	for _, c := range list {
		if total <= budget {
			break
		}
		if err := os.Remove(c.path); err != nil {
			return err
		}
		total -= c.size
	}
	if total > budget {
		return fmt.Errorf("cache capacity exceeded")
	}
	return nil
}

func fileSHA256Context(ctx context.Context, path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, contextReader{ctx, f}); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func fileSHA256(path string) (string, error) { return fileSHA256Context(context.Background(), path) }
func copyFileContext(ctx context.Context, src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err = os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	out, err := os.CreateTemp(filepath.Dir(dst), ".copy-*")
	if err != nil {
		return err
	}
	name := out.Name()
	defer os.Remove(name)
	_, copyErr := io.Copy(out, contextReader{ctx, in})
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.Rename(name, dst)
}
func copyFile(src, dst string) error { return copyFileContext(context.Background(), src, dst) }
