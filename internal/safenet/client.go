package safenet

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// IsPrivateIP checks if an IP belongs to private, loopback, link-local, or unspecified ranges.
func IsPrivateIP(ip net.IP) bool {
	if ip == nil {
		return true
	}

	// Standard Go net.IP checks
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() || ip.IsUnspecified() {
		return true
	}

	// Additional check for 0.0.0.0/8, 169.254.0.0/16 (cloud metadata service)
	ipv4 := ip.To4()
	if ipv4 != nil {
		if ipv4[0] == 0 {
			return true
		}
		if ipv4[0] == 169 && ipv4[1] == 254 {
			return true
		}
		// 100.64.0.0/10 (carrier-grade NAT)
		if ipv4[0] == 100 && (ipv4[1]&0xc0) == 64 {
			return true
		}
	} else {
		// IPv6 Unique Local Address (fc00::/7) or link-local (fe80::/10)
		if len(ip) == net.IPv6len {
			if (ip[0] & 0xfe) == 0xfc { // fc00::/7
				return true
			}
			if ip[0] == 0xfe && (ip[1]&0xc0) == 0x80 { // fe80::/10
				return true
			}
		}
	}

	return false
}

// ValidateURLHost checks if the hostname or IP of a target URL resolves to a private IP.
func ValidateURLHost(targetURL string) error {
	u, err := url.Parse(targetURL)
	if err != nil {
		return fmt.Errorf("URL 格式无效: %w", err)
	}

	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("URL 缺少有效主机名")
	}

	// Check direct IP
	if ip := net.ParseIP(host); ip != nil {
		if IsPrivateIP(ip) {
			return fmt.Errorf("禁止访问私有或内网网络目标: %s", host)
		}
		return nil
	}

	// Resolve hostname
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("解析域名 %s 失败: %w", host, err)
	}

	for _, ip := range ips {
		if IsPrivateIP(ip.IP) {
			return fmt.Errorf("安全拦截: 目标域名 %s 解析到私有/局域网 IP (%s)，已拒绝请求以防范 SSRF", host, ip.IP.String())
		}
	}

	return nil
}

// SafeDialContext creates a net.Dialer that prevents connections to private/internal IPs.
func SafeDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}

	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("DNS 解析失败: %w", err)
	}

	var safeIPs []net.IPAddr
	for _, ip := range ips {
		if IsPrivateIP(ip.IP) {
			return nil, fmt.Errorf("安全拦截: 禁止连接局域网/私有 IP 地址 (%s) 以防范 SSRF 攻击", ip.IP.String())
		}
		safeIPs = append(safeIPs, ip)
	}

	if len(safeIPs) == 0 {
		return nil, fmt.Errorf("未找到有效公开可达的 IP 地址")
	}

	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	// Connect to first safe IP
	targetAddr := net.JoinHostPort(safeIPs[0].IP.String(), port)
	return dialer.DialContext(ctx, network, targetAddr)
}

// NewSafeHTTPClient returns an http.Client equipped with SSRF defense and redirect inspection.
func NewSafeHTTPClient(timeout time.Duration) *http.Client {
	transport := &http.Transport{
		DialContext:           SafeDialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          50,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("重定向次数过多 (超过 10 次)")
			}
			if err := ValidateURLHost(req.URL.String()); err != nil {
				return fmt.Errorf("重定向目标受到安全拦截: %w", err)
			}
			return nil
		},
	}
}

// Call represents an in-flight or completed singleflight request.
type call struct {
	wg  sync.WaitGroup
	val any
	err error
}

// Group implements thread-safe singleflight deduplication.
type Group struct {
	mu sync.Mutex
	m  map[string]*call
}

// Do executes and returns the results of the given function, making
// sure that only one execution is in-flight for a given key at a time.
func (g *Group) Do(key string, fn func() (any, error)) (any, error) {
	g.mu.Lock()
	if g.m == nil {
		g.m = make(map[string]*call)
	}
	if c, ok := g.m[key]; ok {
		g.mu.Unlock()
		c.wg.Wait()
		return c.val, c.err
	}
	c := new(call)
	c.wg.Add(1)
	g.m[key] = c
	g.mu.Unlock()

	c.val, c.err = fn()
	c.wg.Done()

	g.mu.Lock()
	delete(g.m, key)
	g.mu.Unlock()

	return c.val, c.err
}
