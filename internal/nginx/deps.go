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
	"strings"
	"time"
)

// DefaultDepLibraries provides high-quality pre-configured third-party source libraries.
var DefaultDepLibraries = struct {
	OpenSSL []model.DepLibraryInfo `json:"openssl"`
	PCRE    []model.DepLibraryInfo `json:"pcre"`
	Zlib    []model.DepLibraryInfo `json:"zlib"`
}{
	OpenSSL: []model.DepLibraryInfo{
		{
			Name:        "openssl",
			DisplayName: "OpenSSL 3.4.1 (最新主流版，推荐)",
			Version:     "3.4.1",
			SourceURL:   "https://github.com/openssl/openssl/releases/download/openssl-3.4.1/openssl-3.4.1.tar.gz",
			ExpectedSHA: "002a2d6b30b58bf4bea46c43bdd96365aaf8daa6c428782aa4feee06da197df3",
			Description: "包含最新安全修复与 TLS 1.3 现代加密算法优化，由 Nginx 在编译期间直接源码构建静态链接",
			IsDefault:   true,
		},
		{
			Name:        "openssl",
			DisplayName: "OpenSSL 3.0.16 (长期支持 LTS 版)",
			Version:     "3.0.16",
			SourceURL:   "https://github.com/openssl/openssl/releases/download/openssl-3.0.16/openssl-3.0.16.tar.gz",
			ExpectedSHA: "57e03c50feab5d31b152af2b764f10379aecd8ee92f16c985983ce4a99f7ef86",
			Description: "广泛用于企业生产环境的 3.0 LTS 分支，稳定可靠",
			IsDefault:   false,
		},
		{
			Name:        "openssl",
			DisplayName: "OpenSSL 1.1.1w (经典分支 Legacy)",
			Version:     "1.1.1w",
			SourceURL:   "https://github.com/openssl/openssl/releases/download/OpenSSL_1_1_1w/openssl-1.1.1w.tar.gz",
			ExpectedSHA: "cf3098950cb4d853ad95c0841f1f9c6d3dc102dccfcacd521d93925208b76ac8",
			Description: "适合老旧系统或需要特定历史加密特性的经典版本",
			IsDefault:   false,
		},
	},
	PCRE: []model.DepLibraryInfo{
		{
			Name:        "pcre",
			DisplayName: "PCRE2 10.45 (最新主流版，推荐)",
			Version:     "10.45",
			SourceURL:   "https://github.com/PCRE2Project/pcre2/releases/download/pcre2-10.45/pcre2-10.45.tar.gz",
			ExpectedSHA: "0e138387df7835d7403b8351e2226c1377da804e0737db0e071b48f07c9d12ee",
			Description: "官方最新一代 PCRE2 正则引擎源码，支持更强大的 JIT 即时编译，性能极佳",
			IsDefault:   true,
		},
		{
			Name:        "pcre",
			DisplayName: "PCRE2 10.42 (稳定版)",
			Version:     "10.42",
			SourceURL:   "https://github.com/PCRE2Project/pcre2/releases/download/pcre2-10.42/pcre2-10.42.tar.gz",
			ExpectedSHA: "c33b418e3b936ee3153de2c61cc638e7e4fe3156022a5c77d0711bcbb9d64f1f",
			Description: "标准稳定版 PCRE2 源码",
			IsDefault:   false,
		},
	},
	Zlib: []model.DepLibraryInfo{
		{
			Name:        "zlib",
			DisplayName: "zlib 1.3.1 (最新版)",
			Version:     "1.3.1",
			SourceURL:   "https://github.com/madler/zlib/releases/download/v1.3.1/zlib-1.3.1.tar.gz",
			ExpectedSHA: "9a93b2b7dfdac77ceba5a558a580e74667dd6fede4585b91eefb60f03b72df23",
			Description: "标准通用压缩库源码，静态编译后提供高效稳定的 gzip 压缩能力",
			IsDefault:   true,
		},
	},
}

var validDepVersionRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)

func validateDepVersion(version string) error {
	trimmed := strings.TrimSpace(version)
	if trimmed == "" {
		return nil
	}
	if !validDepVersionRe.MatchString(trimmed) || strings.Contains(trimmed, "..") || strings.Contains(trimmed, "/") || strings.Contains(trimmed, "\\") {
		return fmt.Errorf("自定义依赖版本号非法: %q (仅允许字母、数字、点、横杠与下划线，严禁路径穿越)", version)
	}
	return nil
}

// ResolveDepSource resolves a library's download URL and expected SHA256.
func ResolveDepSource(libName, version, customURL string) (*model.DepLibraryInfo, error) {
	if customURL != "" {
		matched, _ := regexp.MatchString(`^https?://`, customURL)
		if !matched {
			return nil, fmt.Errorf("自定义 %s 源码 URL 必须以 http:// 或 https:// 开头", libName)
		}
		ver := strings.TrimSpace(version)
		if ver == "" {
			ver = "custom"
		} else {
			if err := validateDepVersion(ver); err != nil {
				return nil, err
			}
		}
		return &model.DepLibraryInfo{
			Name:        libName,
			DisplayName: fmt.Sprintf("%s (%s)", libName, ver),
			Version:     ver,
			SourceURL:   customURL,
			Description: "用户自定义第三方源码包",
		}, nil
	}

	var list []model.DepLibraryInfo
	switch libName {
	case "openssl":
		list = DefaultDepLibraries.OpenSSL
	case "pcre":
		list = DefaultDepLibraries.PCRE
	case "zlib":
		list = DefaultDepLibraries.Zlib
	default:
		return nil, fmt.Errorf("不支持的依赖库类型: %s", libName)
	}

	if version == "" || version == "latest" || version == "default" {
		for _, item := range list {
			if item.IsDefault {
				return &item, nil
			}
		}
		if len(list) > 0 {
			return &list[0], nil
		}
	}

	for _, item := range list {
		if item.Version == version {
			return &item, nil
		}
	}

	return nil, fmt.Errorf("未找到预设的 %s 版本: %s", libName, version)
}

// DownloadAndVerifyDep downloads a third-party library tarball and returns the downloaded file path and SHA256.
// It uses singleflight to deduplicate concurrent downloads and enforces SSRF defenses.
func DownloadAndVerifyDep(destDir string, info *model.DepLibraryInfo, cacheDir string, logWriter io.Writer) (string, string, error) {
	if err := validateDepVersion(info.Version); err != nil {
		return "", "", fmt.Errorf("安全拦截: %w", err)
	}

	cacheKey := fmt.Sprintf("dep:%s:%s:%s", info.Name, info.Version, info.SourceURL)
	val, err := downloadGroup.Do(cacheKey, func() (any, error) {
		return downloadAndVerifyDepInternal(info, cacheDir, logWriter)
	})
	if err != nil {
		return "", "", err
	}

	res := val.([]string)
	cachePath := res[0]
	hash := res[1]

	safeVer := filepath.Base(info.Version)
	destFileName := fmt.Sprintf("%s-%s.tar.gz", info.Name, safeVer)
	cleanDestDir := filepath.Clean(destDir)
	destPath := filepath.Clean(filepath.Join(cleanDestDir, destFileName))
	relDest, err := filepath.Rel(cleanDestDir, destPath)
	if err != nil || relDest == ".." || strings.HasPrefix(relDest, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("安全拦截: 依赖包目标路径存在越界逃逸风险")
	}
	if err := copyFile(cachePath, destPath); err != nil {
		return "", "", fmt.Errorf("复制依赖包到工作区失败: %w", err)
	}

	return destPath, hash, nil
}

func downloadAndVerifyDepInternal(info *model.DepLibraryInfo, cacheDir string, logWriter io.Writer) ([]string, error) {
	if err := validateDepVersion(info.Version); err != nil {
		return nil, fmt.Errorf("安全拦截: %w", err)
	}

	safeVer := filepath.Base(info.Version)
	fileName := fmt.Sprintf("%s-%s.tar.gz", info.Name, safeVer)
	if info.Version == "custom" || info.ExpectedSHA == "" {
		sum := sha256.Sum256([]byte(info.SourceURL))
		fileName = fmt.Sprintf("%s-%s-%s.tar.gz", info.Name, safeVer, hex.EncodeToString(sum[:])[:8])
	}
	cleanCacheDir := filepath.Clean(cacheDir)
	cachePath := filepath.Clean(filepath.Join(cleanCacheDir, fileName))
	relCache, err := filepath.Rel(cleanCacheDir, cachePath)
	if err != nil || relCache == ".." || strings.HasPrefix(relCache, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("安全拦截: 依赖包缓存路径存在越界逃逸风险")
	}

	_ = os.MkdirAll(cacheDir, 0755)

	// Check local cache
	if _, err := os.Stat(cachePath); err == nil {
		hash, err := fileSHA256(cachePath)
		if err == nil {
			if info.ExpectedSHA == "" || hash == info.ExpectedSHA {
				if logWriter != nil {
					fmt.Fprintf(logWriter, "[DepCache] 从本地缓存命中依赖库: %s (%s, SHA256: %s)\n", info.Name, info.Version, hash)
				}
				return []string{cachePath, hash}, nil
			}
		}
	}

	if err := safenet.ValidateURLHost(info.SourceURL); err != nil {
		return nil, fmt.Errorf("依赖库下载地址安全拦截: %w", err)
	}

	if logWriter != nil {
		fmt.Fprintf(logWriter, "[DepSource] 正在下载第三方依赖库 %s (%s): %s\n", info.Name, info.Version, info.SourceURL)
	}

	client := safenet.NewSafeHTTPClient(90 * time.Second)
	resp, err := client.Get(info.SourceURL)
	if err != nil {
		return nil, fmt.Errorf("下载依赖包 %s 失败: %w", info.Name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("下载依赖包 %s HTTP 异常: %s", info.Name, resp.Status)
	}

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
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("写入依赖包 %s 失败: %w", info.Name, err)
	}

	actualSHA := hex.EncodeToString(hasher.Sum(nil))
	if logWriter != nil {
		fmt.Fprintf(logWriter, "[DepSource] %s 下载完成 (%d bytes)，SHA256: %s\n", info.Name, written, actualSHA)
	}

	if info.ExpectedSHA != "" {
		if actualSHA != info.ExpectedSHA {
			_ = os.Remove(tmpPath)
			return nil, fmt.Errorf("依赖包 %s SHA256 校验不匹配: 期望 %s, 实际 %s", info.Name, info.ExpectedSHA, actualSHA)
		}
		if logWriter != nil {
			fmt.Fprintf(logWriter, "[DepSource] ✔ 官方已知指纹校验通过 (SHA256: %s)\n", actualSHA)
		}
	} else if logWriter != nil {
		fmt.Fprintf(logWriter, "[DepSource] ⚠️ 自定义依赖源码已计算校验和: %s (未绑定官方指纹清单)\n", actualSHA)
	}

	if err := os.Rename(tmpPath, cachePath); err != nil {
		_ = copyFile(tmpPath, cachePath)
		_ = os.Remove(tmpPath)
	}

	return []string{cachePath, actualSHA}, nil
}
