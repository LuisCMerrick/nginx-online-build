package nginx

import (
	"nginx-builder/internal/model"
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
