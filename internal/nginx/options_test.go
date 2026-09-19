package nginx

import (
	"nginx-builder/internal/model"
	"strings"
	"testing"
)

func TestHTTPV2DoesNotMandateHTTPSSL(t *testing.T) {
	args, warns, conflicts, err := ValidateAndBuildArgs([]string{"http_v2"}, nil, nil, nil, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(conflicts) > 0 {
		t.Fatalf("unexpected conflicts: %v", conflicts)
	}

	hasV2 := false
	hasSSL := false
	for _, a := range args {
		if a == "--with-http_v2_module" {
			hasV2 = true
		}
		if a == "--with-http_ssl_module" {
			hasSSL = true
		}
	}
	if !hasV2 {
		t.Fatalf("expected --with-http_v2_module in args: %v", args)
	}
	if hasSSL {
		t.Fatalf("http_v2 should not automatically force --with-http_ssl_module: %v (warns: %v)", args, warns)
	}
}

func TestHTTPV3OpenSSL111wDoesNotForciblyOverrideVersion(t *testing.T) {
	thirdParty := &model.ThirdPartySourcesSpec{
		UseOpenSSLSource: true,
		OpenSSLVersion:   "1.1.1w",
	}
	args, warns, conflicts, err := ValidateAndBuildArgs([]string{"http_v3"}, nil, thirdParty, nil, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = conflicts
	_ = warns
	_ = args

	// OpenSSL 1.1.1w base HTTP/3 (or quictls/compatible branch) should not be silently mutated to 3.4.1 without user consent
	if thirdParty.OpenSSLVersion != "1.1.1w" {
		t.Fatalf("expected OpenSSLVersion to remain 1.1.1w, got %s", thirdParty.OpenSSLVersion)
	}
}

func TestAutoResolveConflictsFalseRejectsConflicts(t *testing.T) {
	// without_http and http_v2 are mutually exclusive
	_, _, conflicts, err := ValidateAndBuildArgs([]string{"without_http", "http_v2"}, nil, nil, nil, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(conflicts) == 0 {
		t.Fatalf("expected conflicts when autoResolveConflicts is false")
	}
}

func TestGzipStaticAndWithoutHttpGzipCanCoexist(t *testing.T) {
	args, _, conflicts, err := ValidateAndBuildArgs([]string{"http_gzip_static", "without_http_gzip"}, nil, nil, nil, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(conflicts) > 0 {
		t.Fatalf("gzip_static and without_http_gzip should not conflict, got conflicts: %v", conflicts)
	}

	hasGzipStatic := false
	hasWithoutGzip := false
	for _, a := range args {
		if a == "--with-http_gzip_static_module" {
			hasGzipStatic = true
		}
		if a == "--without-http_gzip_module" {
			hasWithoutGzip = true
		}
	}
	if !hasGzipStatic || !hasWithoutGzip {
		t.Fatalf("expected both --with-http_gzip_static_module and --without-http_gzip_module in args: %v", args)
	}
}

func TestHttpSliceAndWithoutHttpCacheCanCoexist(t *testing.T) {
	args, _, conflicts, err := ValidateAndBuildArgs([]string{"http_slice", "without_http_cache"}, nil, nil, nil, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(conflicts) > 0 {
		t.Fatalf("http_slice and without_http_cache should not conflict, got conflicts: %v", conflicts)
	}

	hasSlice := false
	hasWithoutCache := false
	for _, a := range args {
		if a == "--with-http_slice_module" {
			hasSlice = true
		}
		if a == "--without-http-cache" {
			hasWithoutCache = true
		}
	}
	if !hasSlice || !hasWithoutCache {
		t.Fatalf("expected both --with-http_slice_module and --without-http-cache in args: %v", args)
	}
}

func TestConflictResolutionIsDeterministicForWithoutHttpStreamWithoutPcre(t *testing.T) {
	// Test 100 times: must yield 100% deterministic output and NEVER drop without_http
	opts := []string{"without_http", "stream", "without_pcre"}
	var firstResult string

	for i := 0; i < 100; i++ {
		args, _, _, err := ValidateAndBuildArgs(opts, nil, nil, nil, true)
		if err != nil {
			t.Fatalf("iteration %d failed: %v", i, err)
		}

		hasWithoutHTTP := false
		for _, a := range args {
			if a == "--without-http" {
				hasWithoutHTTP = true
			}
		}
		if !hasWithoutHTTP {
			t.Fatalf("iteration %d: auto-resolution unexpectedly dropped --without-http! args: %v", i, args)
		}

		joined := strings.Join(args, " ")
		if i == 0 {
			firstResult = joined
		} else if joined != firstResult {
			t.Fatalf("iteration %d: non-deterministic result:\nwant: %s\ngot:  %s", i, firstResult, joined)
		}
	}
}
