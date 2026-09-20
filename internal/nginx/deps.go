package nginx

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/url"
	"nginx-builder/internal/model"
	"nginx-builder/internal/safenet"
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
		parsed, parseErr := url.Parse(customURL)
		if parseErr != nil || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
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

// DownloadAndVerifyDep is the synchronous compatibility entry point.
func DownloadAndVerifyDep(destDir string, info *model.DepLibraryInfo, cacheDir string, writer io.Writer) (string, string, error) {
	return DownloadAndVerifyDepContext(context.Background(), destDir, info, cacheDir, DownloadLimits{}, writer)
}
func DownloadAndVerifyDepContext(ctx context.Context, destDir string, info *model.DepLibraryInfo, cacheDir string, limits DownloadLimits, writer io.Writer) (string, string, error) {
	if info == nil {
		return "", "", fmt.Errorf("invalid dependency")
	}
	if info.Name != "openssl" && info.Name != "pcre" && info.Name != "zlib" {
		return "", "", fmt.Errorf("invalid dependency library")
	}
	if err := validateDepVersion(info.Version); err != nil {
		return "", "", fmt.Errorf("invalid dependency version: %w", err)
	}
	name := fmt.Sprintf("%s-%s.tar.gz", info.Name, info.Version)
	if info.ExpectedSHA == "" {
		sum := sha256.Sum256([]byte(info.SourceURL))
		name = fmt.Sprintf("%s-%s-%x.tar.gz", info.Name, info.Version, sum)
	}
	return fetchSource(ctx, destDir, cacheDir, name, info.SourceURL, info.ExpectedSHA, limits, writer, safenet.NewSafeHTTPClient(90*time.Second))
}
