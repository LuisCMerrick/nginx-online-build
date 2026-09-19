package nginx

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"nginx-builder/internal/model"
	"nginx-builder/internal/safenet"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

var (
	versionCacheMu sync.RWMutex
	cachedVersions []model.VersionInfo
	lastFetchTime  time.Time
	downloadGroup  safenet.Group
)

// DefaultKnownVersions provides high-reliability fallbacks with pre-computed official hashes.
var DefaultKnownVersions = []model.VersionInfo{
	{
		Version:     "1.30.5",
		Channel:     "stable",
		ReleaseDate: "2026-09-15",
		SourceURL:   "https://nginx.org/download/nginx-1.30.5.tar.gz",
		ExpectedSHA: "6c20565aa2325cb82216ae804f4a4ff1875179014759a381c42ddc8e11c4906d",
		IsDefault:   true,
	},
	{
		Version:     "1.31.6",
		Channel:     "mainline",
		ReleaseDate: "2026-09-15",
		SourceURL:   "https://nginx.org/download/nginx-1.31.6.tar.gz",
		ExpectedSHA: "",
		IsDefault:   false,
	},
	{
		Version:     "1.28.3",
		Channel:     "legacy",
		ReleaseDate: "2026-02-10",
		SourceURL:   "https://nginx.org/download/nginx-1.28.3.tar.gz",
		ExpectedSHA: "",
		IsDefault:   false,
	},
}

// GetVersions returns available Nginx versions, refreshing dynamically from nginx.org when needed.
func GetVersions() []model.VersionInfo {
	versionCacheMu.RLock()
	if len(cachedVersions) > 0 && time.Since(lastFetchTime) < 30*time.Minute {
		defer versionCacheMu.RUnlock()
		res := make([]model.VersionInfo, len(cachedVersions))
		copy(res, cachedVersions)
		return res
	}
	versionCacheMu.RUnlock()

	// Fetch or fallback
	versions := fetchOfficialVersions()
	if len(versions) == 0 {
		versions = DefaultKnownVersions
	}

	versionCacheMu.Lock()
	cachedVersions = versions
	lastFetchTime = time.Now()
	versionCacheMu.Unlock()

	res := make([]model.VersionInfo, len(versions))
	copy(res, versions)
	return res
}

// GetVersion returns a specific version or the default stable version.
func GetVersion(v string) (*model.VersionInfo, error) {
	all := GetVersions()
	if v == "" || v == "stable" || v == "latest" {
		for _, info := range all {
			if info.IsDefault || info.Channel == "stable" {
				return &info, nil
			}
		}
	}

	for _, info := range all {
		if info.Version == v {
			return &info, nil
		}
	}

	// Dynamic construct if version matches semver format
	matched, _ := regexp.MatchString(`^1\.\d+\.\d+$`, v)
	if matched {
		return &model.VersionInfo{
			Version:   v,
			Channel:   "custom",
			SourceURL: fmt.Sprintf("https://nginx.org/download/nginx-%s.tar.gz", v),
			IsDefault: false,
		}, nil
	}

	return nil, fmt.Errorf("不支持的 Nginx 版本: %s", v)
}

// fetchOfficialVersions parses https://nginx.org/en/download.html
func fetchOfficialVersions() []model.VersionInfo {
	client := safenet.NewSafeHTTPClient(8 * time.Second)
	resp, err := client.Get("https://nginx.org/en/download.html")
	if err != nil || resp.StatusCode != http.StatusOK {
		return nil
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}
	html := string(bodyBytes)

	var list []model.VersionInfo

	// Extract stable version
	stableRe := regexp.MustCompile(`(?s)<h4>Stable version</h4>.*?<a href="/download/nginx-([\d\.]+)\.tar\.gz">`)
	if m := stableRe.FindStringSubmatch(html); len(m) > 1 {
		ver := m[1]
		list = append(list, model.VersionInfo{
			Version:   ver,
			Channel:   "stable",
			SourceURL: fmt.Sprintf("https://nginx.org/download/nginx-%s.tar.gz", ver),
			IsDefault: true,
		})
	}

	// Extract mainline version
	mainlineRe := regexp.MustCompile(`(?s)<h4>Mainline version</h4>.*?<a href="/download/nginx-([\d\.]+)\.tar\.gz">`)
	if m := mainlineRe.FindStringSubmatch(html); len(m) > 1 {
		ver := m[1]
		list = append(list, model.VersionInfo{
			Version:   ver,
			Channel:   "mainline",
			SourceURL: fmt.Sprintf("https://nginx.org/download/nginx-%s.tar.gz", ver),
			IsDefault: false,
		})
	}

	// Extract legacy versions
	legacyRe := regexp.MustCompile(`href="/download/nginx-([\d\.]+)\.tar\.gz"`)
	matches := legacyRe.FindAllStringSubmatch(html, -1)
	seen := make(map[string]bool)
	for _, v := range list {
		seen[v.Version] = true
	}

	for _, m := range matches {
		if len(m) > 1 {
			ver := m[1]
			if !seen[ver] {
				seen[ver] = true
				list = append(list, model.VersionInfo{
					Version:   ver,
					Channel:   "legacy",
					SourceURL: fmt.Sprintf("https://nginx.org/download/nginx-%s.tar.gz", ver),
					IsDefault: false,
				})
			}
		}
	}

	return list
}

// DownloadAndVerifySource downloads the source tarball (with local caching and singleflight deduplication)
// and computes its sha256 hash.
func DownloadAndVerifySource(targetDir string, versionInfo *model.VersionInfo, cacheDir string, logWriter io.Writer) (string, string, error) {
	cacheKey := fmt.Sprintf("nginx:%s:%s", versionInfo.Version, versionInfo.SourceURL)
	val, err := downloadGroup.Do(cacheKey, func() (any, error) {
		return downloadAndVerifySourceInternal(versionInfo, cacheDir, logWriter)
	})
	if err != nil {
		return "", "", err
	}

	res := val.([]string)
	cachePath := res[0]
	hash := res[1]

	fileName := fmt.Sprintf("nginx-%s.tar.gz", versionInfo.Version)
	destPath := filepath.Join(targetDir, fileName)
	if err := copyFile(cachePath, destPath); err != nil {
		return "", "", fmt.Errorf("复制源码到编译工作目录失败: %w", err)
	}
	return destPath, hash, nil
}

func downloadAndVerifySourceInternal(versionInfo *model.VersionInfo, cacheDir string, logWriter io.Writer) ([]string, error) {
	fileName := fmt.Sprintf("nginx-%s.tar.gz", versionInfo.Version)
	cachePath := filepath.Join(cacheDir, fileName)

	// Ensure cache directory exists
	_ = os.MkdirAll(cacheDir, 0755)

	// Check if already in cache and valid
	if _, err := os.Stat(cachePath); err == nil {
		hash, err := fileSHA256(cachePath)
		if err == nil {
			if versionInfo.ExpectedSHA == "" || hash == versionInfo.ExpectedSHA {
				if logWriter != nil {
					fmt.Fprintf(logWriter, "[Source] 从本地缓存命中源码包: %s (SHA256: %s)\n", cachePath, hash)
				}
				return []string{cachePath, hash}, nil
			}
		}
	}

	if err := safenet.ValidateURLHost(versionInfo.SourceURL); err != nil {
		return nil, fmt.Errorf("源码下载地址安全校验失败: %w", err)
	}

	// Download from official URL
	if logWriter != nil {
		fmt.Fprintf(logWriter, "[Source] 正在从官方源下载: %s\n", versionInfo.SourceURL)
	}

	client := safenet.NewSafeHTTPClient(60 * time.Second)
	resp, err := client.Get(versionInfo.SourceURL)
	if err != nil {
		return nil, fmt.Errorf("下载 Nginx 源码失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("下载 Nginx 源码 HTTP 异常: %s", resp.Status)
	}

	// Write to temporary download file with unique timestamp to prevent race collisions
	tmpPath := fmt.Sprintf("%s.tmp-%d", cachePath, time.Now().UnixNano())
	out, err := os.Create(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("创建临时缓存文件失败: %w", err)
	}

	hasher := sha256.New()
	multiWriter := io.MultiWriter(out, hasher)

	written, err := io.Copy(multiWriter, resp.Body)
	out.Close()
	if err != nil {
		os.Remove(tmpPath)
		return nil, fmt.Errorf("写入源码包数据流失败: %w", err)
	}

	actualSHA := hex.EncodeToString(hasher.Sum(nil))
	if logWriter != nil {
		fmt.Fprintf(logWriter, "[Source] 下载完成 (%d bytes)，SHA256: %s\n", written, actualSHA)
	}

	// Verify expected sha if provided
	if versionInfo.ExpectedSHA != "" {
		if actualSHA != versionInfo.ExpectedSHA {
			os.Remove(tmpPath)
			return nil, fmt.Errorf("源码包 SHA256 校验不匹配: 期望 %s, 实际 %s", versionInfo.ExpectedSHA, actualSHA)
		}
		if logWriter != nil {
			fmt.Fprintf(logWriter, "[Source] ✔ 官方已知指纹校验通过 (SHA256: %s)\n", actualSHA)
		}
	} else if logWriter != nil {
		fmt.Fprintf(logWriter, "[Source] ⚠️ 自定义源码已计算校验和: %s (未绑定官方指纹清单)\n", actualSHA)
	}

	if err := os.Rename(tmpPath, cachePath); err != nil {
		// If rename fails, fallback to copy
		_ = copyFile(tmpPath, cachePath)
		_ = os.Remove(tmpPath)
	}

	return []string{cachePath, actualSHA}, nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
