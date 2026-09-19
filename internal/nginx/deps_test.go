package nginx

import (
	"nginx-builder/internal/model"
	"os"
	"strings"
	"testing"
)

func TestResolveDepSourceSanitizesVersion(t *testing.T) {
	// Attempt path traversal via version string
	maliciousVersions := []string{
		"../../etc",
		"..\\..\\windows",
		"3.0.0/../../../root",
		"1.1.1;rm -rf /",
		"ver with spaces",
	}

	for _, v := range maliciousVersions {
		_, err := ResolveDepSource("openssl", v, "https://example.com/custom-openssl.tar.gz")
		if err == nil {
			t.Errorf("expected error for malicious version string %q", v)
		}
	}
}

func TestDownloadAndVerifyDepRejectsPathTraversal(t *testing.T) {
	destDir, err := os.MkdirTemp("", "test-dep-dest-*")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	defer os.RemoveAll(destDir)

	cacheDir, err := os.MkdirTemp("", "test-dep-cache-*")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	defer os.RemoveAll(cacheDir)

	info := &model.DepLibraryInfo{
		Name:        "openssl",
		Version:     "../../evil",
		SourceURL:   "https://example.com/openssl.tar.gz",
		ExpectedSHA: "dummy",
	}

	_, _, err = DownloadAndVerifyDep(destDir, info, cacheDir, nil)
	if err == nil {
		t.Fatal("expected error for path traversal in version")
	}
	if !strings.Contains(err.Error(), "安全") && !strings.Contains(err.Error(), "非法") && !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("expected safety error, got: %v", err)
	}
}
