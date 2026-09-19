package safenet

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestIsPrivateIP(t *testing.T) {
	tests := []struct {
		ip      string
		private bool
	}{
		{"127.0.0.1", true},
		{"127.0.0.2", true},
		{"10.0.0.1", true},
		{"10.255.255.255", true},
		{"172.16.0.1", true},
		{"172.31.255.255", true},
		{"192.168.1.1", true},
		{"169.254.169.254", true}, // Cloud metadata service
		{"0.0.0.0", true},
		{"::1", true},
		{"fc00::1", true},
		{"fe80::1", true},
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"104.21.5.19", false},
		{"2606:4700::6811:d209", false},
	}

	for _, tt := range tests {
		ip := net.ParseIP(tt.ip)
		if ip == nil {
			t.Fatalf("failed to parse IP %s", tt.ip)
		}
		got := IsPrivateIP(ip)
		if got != tt.private {
			t.Errorf("IsPrivateIP(%q) = %v, want %v", tt.ip, got, tt.private)
		}
	}
}

func TestSafeDialBlocksPrivateIP(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Attempt to dial 127.0.0.1 (local loopback)
	_, err := SafeDialContext(ctx, "tcp", "127.0.0.1:80")
	if err == nil {
		t.Fatal("expected dialing 127.0.0.1 to be blocked by SafeDialContext")
	}
}

func TestCheckRedirectBlocksPrivateIP(t *testing.T) {
	client := NewSafeHTTPClient(2 * time.Second)

	req, _ := http.NewRequest(http.MethodGet, "http://169.254.169.254/latest/meta-data/", nil)
	via := []*http.Request{
		httptest.NewRequest(http.MethodGet, "https://public.example.com/redirect", nil),
	}

	err := client.CheckRedirect(req, via)
	if err == nil {
		t.Fatal("expected redirect to 169.254.169.254 to be blocked")
	}
}

func TestSingleflightGroup(t *testing.T) {
	g := &Group{}
	var calls int32

	var wg sync.WaitGroup
	results := make([]string, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		idx := i
		go func() {
			defer wg.Done()
			v, err := g.Do("test-key", func() (any, error) {
				atomic.AddInt32(&calls, 1)
				time.Sleep(50 * time.Millisecond)
				return "shared-result", nil
			})
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			results[idx] = v.(string)
		}()
	}
	wg.Wait()

	if calls != 1 {
		t.Fatalf("expected function to be called exactly 1 time, called %d times", calls)
	}
	for _, res := range results {
		if res != "shared-result" {
			t.Fatalf("expected result 'shared-result', got %q", res)
		}
	}
}
