package nginx

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"nginx-builder/internal/model"
	"nginx-builder/internal/safenet"
	"regexp"
	"sync"
	"time"
)

var (
	versionCacheMu sync.RWMutex
	cachedVersions []model.VersionInfo
	lastFetchTime  time.Time
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
func GetVersions() []model.VersionInfo { return GetVersionsContext(context.Background()) }
func GetVersionsContext(ctx context.Context) []model.VersionInfo {
	versionCacheMu.RLock()
	if len(cachedVersions) > 0 && time.Since(lastFetchTime) < 30*time.Minute {
		defer versionCacheMu.RUnlock()
		res := make([]model.VersionInfo, len(cachedVersions))
		copy(res, cachedVersions)
		return res
	}
	versionCacheMu.RUnlock()

	// Fetch or fallback
	versions := fetchOfficialVersions(ctx)
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
	return GetVersionContext(context.Background(), v)
}
func GetVersionContext(ctx context.Context, v string) (*model.VersionInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	all := GetVersionsContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
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
func fetchOfficialVersions(ctx context.Context) []model.VersionInfo {
	client := safenet.NewSafeHTTPClient(8 * time.Second)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://nginx.org/en/download.html", nil)
	if err != nil {
		return nil
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
	if err != nil || len(bodyBytes) > 2<<20 {
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

	// Preserve trusted bundled fingerprints when discovery returns the same release.
	for i := range list {
		for _, known := range DefaultKnownVersions {
			if list[i].Version == known.Version && list[i].SourceURL == known.SourceURL {
				list[i].ExpectedSHA = known.ExpectedSHA
			}
		}
	}
	return list
}

// DownloadAndVerifySource is the synchronous compatibility entry point.
func DownloadAndVerifySource(targetDir string, info *model.VersionInfo, cacheDir string, writer io.Writer) (string, string, error) {
	return DownloadAndVerifySourceContext(context.Background(), targetDir, info, cacheDir, DownloadLimits{}, writer)
}
func DownloadAndVerifySourceContext(ctx context.Context, targetDir string, info *model.VersionInfo, cacheDir string, limits DownloadLimits, writer io.Writer) (string, string, error) {
	if info == nil || !regexp.MustCompile(`^1\.\d+\.\d+$`).MatchString(info.Version) {
		return "", "", fmt.Errorf("invalid Nginx version")
	}
	return fetchSource(ctx, targetDir, cacheDir, "nginx-"+info.Version+".tar.gz", info.SourceURL, info.ExpectedSHA, limits, writer, safenet.NewSafeHTTPClient(60*time.Second))
}
