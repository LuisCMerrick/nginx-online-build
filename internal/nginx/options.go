package nginx

import (
	"fmt"
	"nginx-builder/internal/model"
	"strings"
)

// Registry contains all official Nginx configure options with complete dependency and conflict mappings.
var OfficialOptions = []model.NginxOption{
	// ==================== SSL / TLS ====================
	{
		ID:            "http_ssl",
		Name:          "--with-http_ssl_module",
		Flag:          "--with-http_ssl_module",
		Description:   "启用 HTTP SSL/TLS 协议支持 (HTTPS)，支持 SNI 与现代加密套件",
		DefaultState:  false,
		Category:      model.CategorySSL,
		Type:          "bool",
		RequiresLib:   "OpenSSL",
		ConflictsWith: []string{"without_http"},
	},
	{
		ID:            "http_v2",
		Name:          "--with-http_v2_module",
		Flag:          "--with-http_v2_module",
		Description:   "启用 HTTP/2 协议支持",
		DefaultState:  false,
		Category:      model.CategorySSL,
		Type:          "bool",
		DependsOn:     []string{"http_ssl"},
		ConflictsWith: []string{"without_http"},
	},
	{
		ID:            "http_v3",
		Name:          "--with-http_v3_module",
		Flag:          "--with-http_v3_module",
		Description:   "启用 HTTP/3 (QUIC) 协议支持（需 QUIC 兼容的 OpenSSL）",
		DefaultState:  false,
		Category:      model.CategorySSL,
		Type:          "bool",
		DependsOn:     []string{"http_ssl"},
		RequiresLib:   "OpenSSL (with QUIC support)",
		ConflictsWith: []string{"without_http"},
	},
	{
		ID:            "stream_ssl",
		Name:          "--with-stream_ssl_module",
		Flag:          "--with-stream_ssl_module",
		Description:   "启用 Stream TCP/UDP SSL/TLS 终止与代理支持",
		DefaultState:  false,
		Category:      model.CategorySSL,
		Type:          "bool",
		DependsOn:     []string{"stream"},
		RequiresLib:   "OpenSSL",
	},
	{
		ID:            "stream_ssl_preread",
		Name:          "--with-stream_ssl_preread_module",
		Flag:          "--with-stream_ssl_preread_module",
		Description:   "启用 Stream SSL ClientHello 预读模块（支持根据 SNI / ALPN 路由而无需解密）",
		DefaultState:  false,
		Category:      model.CategorySSL,
		Type:          "bool",
		DependsOn:     []string{"stream"},
	},
	{
		ID:            "mail_ssl",
		Name:          "--with-mail_ssl_module",
		Flag:          "--with-mail_ssl_module",
		Description:   "启用 Mail 模块 SSL/TLS 支持 (STARTTLS / SSL)",
		DefaultState:  false,
		Category:      model.CategorySSL,
		Type:          "bool",
		DependsOn:     []string{"mail"},
		RequiresLib:   "OpenSSL",
	},

	// ==================== HTTP Modules ====================
	{
		ID:            "http_realip",
		Name:          "--with-http_realip_module",
		Flag:          "--with-http_realip_module",
		Description:   "启用 RealIP 模块（根据 X-Forwarded-For 等头部重构真实客户端 IP）",
		DefaultState:  false,
		Category:      model.CategoryHTTP,
		Type:          "bool",
		ConflictsWith: []string{"without_http"},
	},
	{
		ID:            "http_addition",
		Name:          "--with-http_addition_module",
		Flag:          "--with-http_addition_module",
		Description:   "启用 Addition 模块（支持在响应体前后追加子请求内容）",
		DefaultState:  false,
		Category:      model.CategoryHTTP,
		Type:          "bool",
		ConflictsWith: []string{"without_http"},
	},
	{
		ID:            "http_sub",
		Name:          "--with-http_sub_module",
		Flag:          "--with-http_sub_module",
		Description:   "启用 Sub 模块（支持对响应内容执行字符串查找替换）",
		DefaultState:  false,
		Category:      model.CategoryHTTP,
		Type:          "bool",
		ConflictsWith: []string{"without_http"},
	},
	{
		ID:            "http_dav",
		Name:          "--with-http_dav_module",
		Flag:          "--with-http_dav_module",
		Description:   "启用 WebDAV 模块（提供 PUT、DELETE、MKCOL、COPY、MOVE 方法支持）",
		DefaultState:  false,
		Category:      model.CategoryHTTP,
		Type:          "bool",
		ConflictsWith: []string{"without_http"},
	},
	{
		ID:            "http_flv",
		Name:          "--with-http_flv_module",
		Flag:          "--with-http_flv_module",
		Description:   "启用 FLV 流媒体伪流式分发支持",
		DefaultState:  false,
		Category:      model.CategoryHTTP,
		Type:          "bool",
		ConflictsWith: []string{"without_http"},
	},
	{
		ID:            "http_mp4",
		Name:          "--with-http_mp4_module",
		Flag:          "--with-http_mp4_module",
		Description:   "启用 MP4 流媒体分发支持（支持 start/end 时间戳精准拖拽）",
		DefaultState:  false,
		Category:      model.CategoryHTTP,
		Type:          "bool",
		ConflictsWith: []string{"without_http"},
	},
	{
		ID:            "http_gunzip",
		Name:          "--with-http_gunzip_module",
		Flag:          "--with-http_gunzip_module",
		Description:   "启用 Gunzip 模块（为不支持 gzip 的客户端自动解压已压缩响应）",
		DefaultState:  false,
		Category:      model.CategoryHTTP,
		Type:          "bool",
		RequiresLib:   "zlib",
		ConflictsWith: []string{"without_http", "without_http_gzip"},
	},
	{
		ID:            "http_gzip_static",
		Name:          "--with-http_gzip_static_module",
		Flag:          "--with-http_gzip_static_module",
		Description:   "启用 Gzip Static 模块（直接发送磁盘上预压缩的 .gz 文件）",
		DefaultState:  false,
		Category:      model.CategoryHTTP,
		Type:          "bool",
		RequiresLib:   "zlib",
		ConflictsWith: []string{"without_http", "without_http_gzip"},
	},
	{
		ID:            "http_auth_request",
		Name:          "--with-http_auth_request_module",
		Flag:          "--with-http_auth_request_module",
		Description:   "启用 Auth Request 模块（基于外部子请求鉴权，如 OAuth 校验）",
		DefaultState:  false,
		Category:      model.CategoryHTTP,
		Type:          "bool",
		ConflictsWith: []string{"without_http"},
	},
	{
		ID:            "http_random_index",
		Name:          "--with-http_random_index_module",
		Flag:          "--with-http_random_index_module",
		Description:   "启用 Random Index 模块（从目录下随机选择主页展示）",
		DefaultState:  false,
		Category:      model.CategoryHTTP,
		Type:          "bool",
		ConflictsWith: []string{"without_http"},
	},
	{
		ID:            "http_secure_link",
		Name:          "--with-http_secure_link_module",
		Flag:          "--with-http_secure_link_module",
		Description:   "启用 Secure Link 模块（基于 MD5 Hash 与过期时间戳防盗链）",
		DefaultState:  false,
		Category:      model.CategoryHTTP,
		Type:          "bool",
		RequiresLib:   "OpenSSL (MD5)",
		ConflictsWith: []string{"without_http"},
	},
	{
		ID:            "http_degradation",
		Name:          "--with-http_degradation_module",
		Flag:          "--with-http_degradation_module",
		Description:   "启用 Degradation 模块（系统内存不足时降级返回 204 或 444）",
		DefaultState:  false,
		Category:      model.CategoryHTTP,
		Type:          "bool",
		ConflictsWith: []string{"without_http"},
	},
	{
		ID:            "http_slice",
		Name:          "--with-http_slice_module",
		Flag:          "--with-http_slice_module",
		Description:   "启用 Slice 模块（大文件分片请求与缓存，需 HTTP 缓存协同）",
		DefaultState:  false,
		Category:      model.CategoryHTTP,
		Type:          "bool",
		ConflictsWith: []string{"without_http", "without_http_cache"},
	},
	{
		ID:            "http_stub_status",
		Name:          "--with-http_stub_status_module",
		Flag:          "--with-http_stub_status_module",
		Description:   "启用 Stub Status 模块（提供基础运行状态指标监控页面）",
		DefaultState:  false,
		Category:      model.CategoryHTTP,
		Type:          "bool",
		ConflictsWith: []string{"without_http"},
	},
	{
		ID:            "http_xslt",
		Name:          "--with-http_xslt_module",
		Flag:          "--with-http_xslt_module",
		Description:   "启用 XML XSLT 响应转换模块",
		DefaultState:  false,
		Category:      model.CategoryHTTP,
		Type:          "bool",
		RequiresLib:   "libxml2, libxslt",
		ConflictsWith: []string{"without_http"},
	},
	{
		ID:            "http_image_filter",
		Name:          "--with-http_image_filter_module",
		Flag:          "--with-http_image_filter_module",
		Description:   "启用 Image Filter 模块（实时缩放、裁剪图像）",
		DefaultState:  false,
		Category:      model.CategoryHTTP,
		Type:          "bool",
		RequiresLib:   "libgd",
		ConflictsWith: []string{"without_http"},
	},
	{
		ID:            "http_geoip",
		Name:          "--with-http_geoip_module",
		Flag:          "--with-http_geoip_module",
		Description:   "启用 GeoIP IP 地理位置解析模块",
		DefaultState:  false,
		Category:      model.CategoryHTTP,
		Type:          "bool",
		RequiresLib:   "GeoIP C library",
		ConflictsWith: []string{"without_http"},
	},

	// ==================== Stream (四层 TCP/UDP) ====================
	{
		ID:           "stream",
		Name:         "--with-stream",
		Flag:         "--with-stream",
		Description:  "启用 Stream 四层 TCP/UDP 反向代理与负载均衡核心引擎",
		DefaultState: false,
		Category:     model.CategoryStream,
		Type:         "bool",
	},
	{
		ID:           "stream_realip",
		Name:         "--with-stream_realip_module",
		Flag:         "--with-stream_realip_module",
		Description:  "启用 Stream PROXY Protocol 真实 IP 提取解析模块",
		DefaultState: false,
		Category:     model.CategoryStream,
		Type:         "bool",
		DependsOn:    []string{"stream"},
	},
	{
		ID:           "stream_geoip",
		Name:         "--with-stream_geoip_module",
		Flag:         "--with-stream_geoip_module",
		Description:  "启用 Stream GeoIP 地理位置路由模块",
		DefaultState: false,
		Category:     model.CategoryStream,
		Type:         "bool",
		DependsOn:    []string{"stream"},
		RequiresLib:  "GeoIP C library",
	},

	// ==================== Mail 邮件代理 ====================
	{
		ID:           "mail",
		Name:         "--with-mail",
		Flag:         "--with-mail",
		Description:  "启用 Mail 邮件代理支持 (POP3 / IMAP / SMTP 反向代理)",
		DefaultState: false,
		Category:     model.CategoryMail,
		Type:         "bool",
	},

	// ==================== 性能与并发 ====================
	{
		ID:           "threads",
		Name:         "--with-threads",
		Flag:         "--with-threads",
		Description:  "启用异步线程池 (Thread Pool) 提升大文件读取并发与阻塞操作解耦",
		DefaultState: false,
		Category:     model.CategoryPerformance,
		Type:         "bool",
	},
	{
		ID:           "file_aio",
		Name:         "--with-file-aio",
		Flag:         "--with-file-aio",
		Description:  "启用 Linux 内核异步 I/O (File AIO) 增强大文件磁盘写入性能",
		DefaultState: false,
		Category:     model.CategoryPerformance,
		Type:         "bool",
	},
	{
		ID:            "pcre_jit",
		Name:          "--with-pcre-jit",
		Flag:          "--with-pcre-jit",
		Description:   "启用 PCRE 正则表达式 JIT 即时编译，加速正则表达式匹配速度",
		DefaultState:  false,
		Category:      model.CategoryPerformance,
		Type:          "bool",
		ConflictsWith: []string{"without_pcre"},
	},
	{
		ID:           "compat",
		Name:         "--with-compat",
		Flag:         "--with-compat",
		Description:  "启用动态模块二进制兼容层（允许加载外部符合兼容协议的 .so 动态模块）",
		DefaultState: false,
		Category:     model.CategoryPerformance,
		Type:         "bool",
	},

	// ==================== 调试与诊断 ====================
	{
		ID:           "debug",
		Name:         "--with-debug",
		Flag:         "--with-debug",
		Description:  "启用 debug 调试日志记录支持 (error_log ... debug)",
		DefaultState: false,
		Category:     model.CategoryDebug,
		Type:         "bool",
	},
	{
		ID:           "cpp_test",
		Name:         "--with-cpp_test_module",
		Flag:         "--with-cpp_test_module",
		Description:  "启用 C++ 测试兼容模块",
		DefaultState: false,
		Category:     model.CategoryDebug,
		Type:         "bool",
	},

	// ==================== 系统与事件模块 ====================
	{
		ID:            "select_module",
		Name:          "--with-select_module",
		Flag:          "--with-select_module",
		Description:   "强制编译 select 事件驱动模块",
		DefaultState:  false,
		Category:      model.CategorySystem,
		Type:          "bool",
		ConflictsWith: []string{"without_select_module"},
	},
	{
		ID:            "without_select_module",
		Name:          "--without-select_module",
		Flag:          "--without-select_module",
		Description:   "显式禁用 select 事件驱动模块",
		DefaultState:  false,
		Category:      model.CategorySystem,
		Type:          "bool",
		ConflictsWith: []string{"select_module"},
	},
	{
		ID:            "poll_module",
		Name:          "--with-poll_module",
		Flag:          "--with-poll_module",
		Description:   "强制编译 poll 事件驱动模块",
		DefaultState:  false,
		Category:      model.CategorySystem,
		Type:          "bool",
		ConflictsWith: []string{"without_poll_module"},
	},
	{
		ID:            "without_poll_module",
		Name:          "--without-poll_module",
		Flag:          "--without-poll_module",
		Description:   "显式禁用 poll 事件驱动模块",
		DefaultState:  false,
		Category:      model.CategorySystem,
		Type:          "bool",
		ConflictsWith: []string{"poll_module"},
	},

	// ==================== 核心子系统与功能禁用 (--without-xxx) ====================
	{
		ID:            "without_http",
		Name:          "--without-http",
		Flag:          "--without-http",
		Description:   "完全禁用 HTTP 服务核心（仅用于纯四层 Stream 或 Mail 代理）",
		DefaultState:  false,
		Category:      model.CategoryOther,
		Type:          "bool",
		ConflictsWith: []string{
			"http_ssl", "http_v2", "http_v3", "http_realip", "http_addition",
			"http_sub", "http_dav", "http_flv", "http_mp4", "http_gunzip",
			"http_gzip_static", "http_auth_request", "http_random_index", "http_secure_link",
			"http_degradation", "http_slice", "http_stub_status", "http_xslt",
			"http_image_filter", "http_geoip", "without_http_gzip", "without_http_rewrite",
			"without_http_proxy", "without_http_fastcgi", "without_http_uwsgi",
			"without_http_scgi", "without_http_grpc", "without_http_cache",
		},
	},
	{
		ID:            "without_http_cache",
		Name:          "--without-http-cache",
		Flag:          "--without-http-cache",
		Description:   "完全禁用 HTTP 缓存机制（与依赖缓存的 http_slice 模块互斥）",
		DefaultState:  false,
		Category:      model.CategoryOther,
		Type:          "bool",
		ConflictsWith: []string{"without_http", "http_slice"},
	},
	{
		ID:            "without_pcre",
		Name:          "--without-pcre",
		Flag:          "--without-pcre",
		Description:   "禁用 PCRE 正则引擎（必须同时禁用 rewrite 模块，且与 PCRE JIT 互斥）",
		DefaultState:  false,
		Category:      model.CategoryOther,
		Type:          "bool",
		ConflictsWith: []string{"pcre_jit"},
	},
	{
		ID:            "without_http_gzip",
		Name:          "--without-http_gzip_module",
		Flag:          "--without-http_gzip_module",
		Description:   "禁用 HTTP Gzip 响应实时压缩模块（与 gzip_static、gunzip 模块互斥）",
		DefaultState:  false,
		Category:      model.CategoryOther,
		Type:          "bool",
		ConflictsWith: []string{"without_http", "http_gzip_static", "http_gunzip"},
	},
	{
		ID:            "without_http_rewrite",
		Name:          "--without-http_rewrite_module",
		Flag:          "--without-http_rewrite_module",
		Description:   "禁用 HTTP Rewrite 重写重定向模块",
		DefaultState:  false,
		Category:      model.CategoryOther,
		Type:          "bool",
		ConflictsWith: []string{"without_http"},
	},
	{
		ID:            "without_http_proxy",
		Name:          "--without-http_proxy_module",
		Flag:          "--without-http_proxy_module",
		Description:   "禁用 HTTP Proxy 反向代理模块",
		DefaultState:  false,
		Category:      model.CategoryOther,
		Type:          "bool",
		ConflictsWith: []string{"without_http"},
	},
	{
		ID:            "without_http_fastcgi",
		Name:          "--without-http_fastcgi_module",
		Flag:          "--without-http_fastcgi_module",
		Description:   "禁用 FastCGI (如 PHP-FPM) 代理模块",
		DefaultState:  false,
		Category:      model.CategoryOther,
		Type:          "bool",
		ConflictsWith: []string{"without_http"},
	},
	{
		ID:            "without_http_uwsgi",
		Name:          "--without-http_uwsgi_module",
		Flag:          "--without-http_uwsgi_module",
		Description:   "禁用 uWSGI (如 Python) 代理模块",
		DefaultState:  false,
		Category:      model.CategoryOther,
		Type:          "bool",
		ConflictsWith: []string{"without_http"},
	},
	{
		ID:            "without_http_scgi",
		Name:          "--without-http_scgi_module",
		Flag:          "--without-http_scgi_module",
		Description:   "禁用 SCGI 代理模块",
		DefaultState:  false,
		Category:      model.CategoryOther,
		Type:          "bool",
		ConflictsWith: []string{"without_http"},
	},
	{
		ID:            "without_http_grpc",
		Name:          "--without-http_grpc_module",
		Flag:          "--without-http_grpc_module",
		Description:   "禁用 gRPC 代理模块",
		DefaultState:  false,
		Category:      model.CategoryOther,
		Type:          "bool",
		ConflictsWith: []string{"without_http"},
	},
}

// AllowedPathOptions maps safe path configure options.
var AllowedPathOptions = map[string]struct {
	Flag        string `json:"flag"`
	Default     string `json:"default"`
	Description string `json:"description"`
}{
	"prefix": {
		Flag:        "--prefix",
		Default:     "/usr/local/nginx",
		Description: "Nginx 安装主前缀路径",
	},
	"sbin-path": {
		Flag:        "--sbin-path",
		Default:     "/usr/local/nginx/sbin/nginx",
		Description: "Nginx 二进制执行文件绝对路径",
	},
	"conf-path": {
		Flag:        "--conf-path",
		Default:     "/usr/local/nginx/conf/nginx.conf",
		Description: "Nginx 主配置文件路径",
	},
	"error-log-path": {
		Flag:        "--error-log-path",
		Default:     "/usr/local/nginx/logs/error.log",
		Description: "默认全局错误日志路径",
	},
	"http-log-path": {
		Flag:        "--http-log-path",
		Default:     "/usr/local/nginx/logs/access.log",
		Description: "默认 HTTP 访问日志路径",
	},
	"pid-path": {
		Flag:        "--pid-path",
		Default:     "/usr/local/nginx/logs/nginx.pid",
		Description: "主进程 PID 文件路径",
	},
	"lock-path": {
		Flag:        "--lock-path",
		Default:     "/usr/local/nginx/logs/nginx.lock",
		Description: "主进程文件锁路径",
	},
	"user": {
		Flag:        "--user",
		Default:     "nobody",
		Description: "工作进程运行非特权系统用户",
	},
	"group": {
		Flag:        "--group",
		Default:     "nogroup",
		Description: "工作进程运行非特权系统组",
	},
}

// FindOption returns an option by its ID.
func FindOption(id string) *model.NginxOption {
	for i := range OfficialOptions {
		if OfficialOptions[i].ID == id {
			return &OfficialOptions[i]
		}
	}
	return nil
}

// ValidateAndBuildArgs processes input option IDs, path overrides, and third-party sources,
// enforces dependencies/conflicts, and produces safe, deterministic configure arguments.
// When autoResolveConflicts is true, it safely reconciles conflicting options (e.g. keeping affirmative features
// over negative exclusions) and appends explanations to warnings.
// When autoResolveConflicts is false, it returns detected conflicts in the conflicts slice without failing with a fatal error.
func ValidateAndBuildArgs(
	selectedIDs []string,
	pathOverrides map[string]string,
	thirdParty *model.ThirdPartySourcesSpec,
	resolvedDepDirs map[string]string, // map of "openssl" -> "/path/to/extracted/openssl"
	autoResolveConflicts bool,
) ([]string, []string, []string, error) {
	selectedMap := make(map[string]bool)
	for _, id := range selectedIDs {
		trimmed := strings.TrimSpace(id)
		if trimmed != "" {
			selectedMap[trimmed] = true
		}
	}

	var warnings []string
	var conflicts []string
	conflictPairsSeen := make(map[string]bool)

	// Check if all major subsystems are disabled
	if selectedMap["without_http"] && !selectedMap["stream"] && !selectedMap["mail"] {
		msg := "子系统提示: 已禁用 HTTP 子系统 (--without-http)，但未启用 Stream 或 Mail 代理中的任意一项。Nginx 至少需要一个服务引擎"
		if autoResolveConflicts {
			selectedMap["stream"] = true
			warnings = append(warnings, msg+"（已自动为您启用 Stream 四层代理引擎以确保可用性）")
		} else {
			conflicts = append(conflicts, msg)
		}
	}

	// Third-party OpenSSL source automatic SSL enablement
	if thirdParty != nil && thirdParty.UseOpenSSLSource {
		if !selectedMap["http_ssl"] && !selectedMap["stream_ssl"] && !selectedMap["mail_ssl"] {
			selectedMap["http_ssl"] = true
			warnings = append(warnings, "由于启用了 OpenSSL 源码静态编译，已自动激活依赖的 --with-http_ssl_module")
		}
		// OpenSSL 1.1.1w does NOT support HTTP/3 (QUIC)
		if thirdParty.OpenSSLVersion == "1.1.1w" && selectedMap["http_v3"] {
			msg := "版本互斥提示: OpenSSL 1.1.1w 不提供 QUIC 协议 API 支持，无法与 --with-http_v3_module (HTTP/3) 配合编译"
			if autoResolveConflicts {
				thirdParty.OpenSSLVersion = "3.4.1"
				warnings = append(warnings, msg+"（已自动将 OpenSSL 源码版本升级为支持 QUIC 的 3.4.1 稳定版）")
			} else {
				conflicts = append(conflicts, msg+"，建议升级至 OpenSSL 3.4.1 或取消 HTTP/3")
			}
		}
	}

	// Third-party PCRE source conflict with without_pcre
	if thirdParty != nil && thirdParty.UsePCRESource && selectedMap["without_pcre"] {
		msg := "参数互斥提示: 同时指定了禁用 PCRE (--without-pcre) 与启用 PCRE 源码静态编译 (--with-pcre)"
		if autoResolveConflicts {
			delete(selectedMap, "without_pcre")
			warnings = append(warnings, msg+"（已自动移除 --without-pcre 并保留源码编译）")
		} else {
			conflicts = append(conflicts, msg)
		}
	}

	// If without_pcre is chosen, Nginx auto/lib/conf mandates without_http_rewrite_module
	if selectedMap["without_pcre"] && !selectedMap["without_http_rewrite"] {
		selectedMap["without_http_rewrite"] = true
		warnings = append(warnings, "Nginx 官方规定: 禁用 PCRE 时必须禁用 HTTP Rewrite 模块，已自动添加 --without-http_rewrite_module")
	}

	// 1. Dependency resolution: auto-satisfy parent dependencies
	for id := range selectedMap {
		opt := FindOption(id)
		if opt == nil {
			return nil, nil, nil, fmt.Errorf("非法编译参数标识: %s，不在官方白名单中", id)
		}
		for _, dep := range opt.DependsOn {
			if !selectedMap[dep] {
				depOpt := FindOption(dep)
				depName := dep
				if depOpt != nil {
					depName = depOpt.Name
				}
				selectedMap[dep] = true
				warnings = append(warnings, fmt.Sprintf("模块 %s 依赖于 %s，已自动满足并启用", opt.Name, depName))
			}
		}
	}

	// 2. Conflict detection and smart auto-resolution
	for id := range selectedMap {
		opt := FindOption(id)
		if opt == nil {
			continue
		}
		for _, conf := range opt.ConflictsWith {
			if selectedMap[conf] {
				pairKey := id + ":" + conf
				if conf < id {
					pairKey = conf + ":" + id
				}
				if conflictPairsSeen[pairKey] {
					continue
				}
				conflictPairsSeen[pairKey] = true

				confOpt := FindOption(conf)
				confName := conf
				if confOpt != nil {
					confName = confOpt.Name
				}

				if autoResolveConflicts {
					// Reconcile: prefer affirmative over negative exclusion
					if strings.HasPrefix(id, "without_") && !strings.HasPrefix(conf, "without_") {
						delete(selectedMap, id)
						warnings = append(warnings, fmt.Sprintf("检测到选项互斥: 【%s】与【%s】，已自动保留功能项【%s】并移除禁用参数", opt.Name, confName, confName))
					} else if strings.HasPrefix(conf, "without_") && !strings.HasPrefix(id, "without_") {
						delete(selectedMap, conf)
						warnings = append(warnings, fmt.Sprintf("检测到选项互斥: 【%s】与【%s】，已自动保留功能项【%s】并移除禁用参数", opt.Name, confName, opt.Name))
					} else {
						// Otherwise drop the secondary one
						delete(selectedMap, conf)
						warnings = append(warnings, fmt.Sprintf("检测到选项互斥: 【%s】与【%s】，已自动保留【%s】", opt.Name, confName, opt.Name))
					}
				} else {
					conflicts = append(conflicts, fmt.Sprintf("参数互斥: 【%s】与【%s】互为排斥选项，同时配置可能导致编译失败", opt.Name, confName))
				}
			}
		}
	}

	// 3. Construct arguments strictly from whitelist
	var args []string

	// Handle prefix and paths first
	prefix := "/usr/local/nginx"
	if p, ok := pathOverrides["prefix"]; ok && isValidPath(p) {
		prefix = strings.TrimSpace(p)
	}
	args = append(args, fmt.Sprintf("--prefix=%s", prefix))

	for key, meta := range AllowedPathOptions {
		if key == "prefix" {
			continue
		}
		if val, ok := pathOverrides[key]; ok {
			trimmed := strings.TrimSpace(val)
			if trimmed != "" && isValidPath(trimmed) {
				// If without_http is set, skip http paths
				if selectedMap["without_http"] && strings.HasPrefix(key, "http-") {
					continue
				}
				args = append(args, fmt.Sprintf("%s=%s", meta.Flag, trimmed))
			}
		}
	}

	// Append selected module flags in deterministic order
	for _, opt := range OfficialOptions {
		if selectedMap[opt.ID] {
			args = append(args, opt.Flag)
		}
	}

	// Append Third-party Source dependencies (--with-openssl, --with-pcre, --with-zlib)
	if thirdParty != nil {
		// OpenSSL Source
		if thirdParty.UseOpenSSLSource {
			opensslDir := "/path/to/openssl-src"
			if resolvedDepDirs != nil && resolvedDepDirs["openssl"] != "" {
				opensslDir = resolvedDepDirs["openssl"]
			} else if thirdParty.OpenSSLVersion != "" {
				opensslDir = fmt.Sprintf("/deps/openssl-%s", thirdParty.OpenSSLVersion)
			}
			args = append(args, fmt.Sprintf("--with-openssl=%s", opensslDir))
			if thirdParty.OpenSSLOpt != "" && isValidOpt(thirdParty.OpenSSLOpt) {
				args = append(args, fmt.Sprintf("--with-openssl-opt=%s", strings.TrimSpace(thirdParty.OpenSSLOpt)))
			}
		}

		// PCRE Source
		if thirdParty.UsePCRESource && !selectedMap["without_pcre"] {
			pcreDir := "/path/to/pcre2-src"
			if resolvedDepDirs != nil && resolvedDepDirs["pcre"] != "" {
				pcreDir = resolvedDepDirs["pcre"]
			} else if thirdParty.PCREVersion != "" {
				pcreDir = fmt.Sprintf("/deps/pcre2-%s", thirdParty.PCREVersion)
			}
			args = append(args, fmt.Sprintf("--with-pcre=%s", pcreDir))
			if thirdParty.PCREOpt != "" && isValidOpt(thirdParty.PCREOpt) {
				args = append(args, fmt.Sprintf("--with-pcre-opt=%s", strings.TrimSpace(thirdParty.PCREOpt)))
			}
		}

		// Zlib Source
		if thirdParty.UseZlibSource {
			zlibDir := "/path/to/zlib-src"
			if resolvedDepDirs != nil && resolvedDepDirs["zlib"] != "" {
				zlibDir = resolvedDepDirs["zlib"]
			} else if thirdParty.ZlibVersion != "" {
				zlibDir = fmt.Sprintf("/deps/zlib-%s", thirdParty.ZlibVersion)
			}
			args = append(args, fmt.Sprintf("--with-zlib=%s", zlibDir))
			if thirdParty.ZlibOpt != "" && isValidOpt(thirdParty.ZlibOpt) {
				args = append(args, fmt.Sprintf("--with-zlib-opt=%s", strings.TrimSpace(thirdParty.ZlibOpt)))
			}
		}
	}

	return args, warnings, conflicts, nil
}

// isValidPath ensures paths do not contain dangerous characters or shell injection tokens.
func isValidPath(p string) bool {
	if strings.ContainsAny(p, "\n\r\t;&|`$<>(){}[]*?\\'\"") {
		return false
	}
	if !strings.HasPrefix(p, "/") && !isAlphaNumWord(p) {
		return false
	}
	return len(p) < 256
}

// isValidOpt validates optional compiler flags like enable-tls1_3, no-deprecated, etc.
func isValidOpt(opt string) bool {
	if strings.ContainsAny(opt, "\n\r\t;&|`$<>(){}[]*?\\'\"") {
		return false
	}
	return len(opt) < 128
}

func isAlphaNumWord(s string) bool {
	if len(s) == 0 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') && r != '_' && r != '-' {
			return false
		}
	}
	return true
}
