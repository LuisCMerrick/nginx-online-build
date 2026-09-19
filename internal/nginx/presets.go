package nginx

// ParameterPreset represents a curated configuration template for common Nginx deployment scenarios.
type ParameterPreset struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	DisplayName string   `json:"display_name"`
	Description string   `json:"description"`
	Options     []string `json:"options"`
}

// ParameterPresets defines production-grade templates tailored for different workload architectures.
var ParameterPresets = []ParameterPreset{
	{
		ID:          "modern_web",
		Name:        "Standard Web & Reverse Proxy (Recommended)",
		DisplayName: "🌐 标准现代 Web 与反向代理 (推荐)",
		Description: "生产主流推荐：启用 HTTPS (SSL/TLS)、HTTP/2、四层 Stream 转发、真实 IP 提取、预压缩静态文件直接分发 (Gzip Static)、状态监控及异步线程池加速。",
		Options: []string{
			"http_ssl",
			"http_v2",
			"stream",
			"stream_ssl",
			"http_realip",
			"http_gzip_static",
			"http_stub_status",
			"pcre_jit",
			"threads",
		},
	},
	{
		ID:          "full_featured",
		Name:        "Full-Featured (All Official Modules)",
		DisplayName: "🚀 全功能官方模块合集",
		Description: "启用官方绝大多数主流功能：包含 HTTP/2、HTTP/3 (QUIC)、四层全代理、SNI 预读、XSLT、图片剪裁、GeoIP、分片缓存、安全防盗链及动态模块兼容层。",
		Options: []string{
			"http_ssl",
			"http_v2",
			"http_v3",
			"stream",
			"stream_ssl",
			"stream_ssl_preread",
			"stream_realip",
			"http_realip",
			"http_addition",
			"http_sub",
			"http_gunzip",
			"http_gzip_static",
			"http_auth_request",
			"http_secure_link",
			"http_slice",
			"http_stub_status",
			"threads",
			"file_aio",
			"pcre_jit",
			"compat",
		},
	},
	{
		ID:          "minimal",
		Name:        "Minimal & Tiny Server",
		DisplayName: "🪶 极简轻量服务器 (剥离不常用协议)",
		Description: "剥离 FastCGI、uWSGI、SCGI、gRPC 等不需要的网关协议，编译极小体积的高性能轻量 Nginx，适合纯前端分发或轻量代理。",
		Options: []string{
			"without_http_fastcgi",
			"without_http_uwsgi",
			"without_http_scgi",
			"without_http_grpc",
			"pcre_jit",
		},
	},
	{
		ID:          "media_streaming",
		Name:        "Media Streaming (HLS/MP4/FLV)",
		DisplayName: "🎬 音视频流媒体与大文件分发",
		Description: "针对音视频点播与大文件分发优化：包含 MP4 关键帧拖拽寻道、FLV 伪流媒体、大文件分片 Slice 缓存、防盗链 Secure Link 以及高并发异步 I/O (File AIO)。",
		Options: []string{
			"http_ssl",
			"http_v2",
			"http_flv",
			"http_mp4",
			"http_slice",
			"http_secure_link",
			"http_realip",
			"threads",
			"file_aio",
			"pcre_jit",
		},
	},
	{
		ID:          "l4_gateway",
		Name:        "L4 TCP/UDP Load Balancer",
		DisplayName: "🔀 四层 TCP/UDP 负载均衡网关",
		Description: "专注于高性能四层流代理：支持 TCP/UDP 负载转发、SSL 终止与透传、SNI / ALPN 预读解析 (无需解密即可按域名路由) 及 Proxy Protocol 客户端真实 IP 传递。",
		Options: []string{
			"stream",
			"stream_ssl",
			"stream_ssl_preread",
			"stream_realip",
			"threads",
			"pcre_jit",
			"http_ssl",
			"http_stub_status",
		},
	},
	{
		ID:          "security_hardened",
		Name:        "Security Hardened & Access Control",
		DisplayName: "🛡️ 安全访问控制与鉴权加固",
		Description: "注重传输安全与权限拦截：启用 HTTPS/QUIC 加密通道、子请求外部统一鉴权 (Auth Request)、带时效与哈希签名的防盗链 (Secure Link) 以及真实客户端 IP 提取。",
		Options: []string{
			"http_ssl",
			"http_v2",
			"http_v3",
			"http_realip",
			"http_auth_request",
			"http_secure_link",
			"http_stub_status",
			"pcre_jit",
		},
	},
	{
		ID:          "dynamic_compat",
		Name:        "Dynamic Modules Compatible",
		DisplayName: "🧩 动态模块二进制兼容 (compat)",
		Description: "启用 --with-compat 保持二进制 ABI 兼容性，方便后续热加载外部第三方 .so 动态模块，并启用异步线程池与 PCRE JIT 即时编译加速。",
		Options: []string{
			"compat",
			"http_ssl",
			"http_v2",
			"stream",
			"stream_ssl",
			"http_realip",
			"threads",
			"file_aio",
			"pcre_jit",
		},
	},
}
