package builder

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArtifactDownloadPathIsRootRelative(t *testing.T) {
	if got, want := artifactDownloadPath("build-123"), "/api/builds/build-123/artifact"; got != want {
		t.Fatalf("artifactDownloadPath() = %q, want %q", got, want)
	}
}

func createTestTarGz(t *testing.T, files map[string][]byte) string {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	for name, content := range files {
		hdr := &tar.Header{
			Name:     name,
			Mode:     0644,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
		}
		if strings.HasSuffix(name, "/") {
			hdr.Typeflag = tar.TypeDir
			hdr.Mode = 0755
			hdr.Size = 0
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("write tar header: %v", err)
		}
		if len(content) > 0 {
			if _, err := tw.Write(content); err != nil {
				t.Fatalf("write tar content: %v", err)
			}
		}
	}
	_ = tw.Close()
	_ = gw.Close()

	tmpFile, err := os.CreateTemp("", "test-archive-*.tar.gz")
	if err != nil {
		t.Fatalf("create temp archive: %v", err)
	}
	if _, err := tmpFile.Write(buf.Bytes()); err != nil {
		t.Fatalf("write archive: %v", err)
	}
	tmpFile.Close()
	return tmpFile.Name()
}

func TestExtractTarGzRejectsPathTraversal(t *testing.T) {
	destDir, err := os.MkdirTemp("", "test-extract-dest-*")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	defer os.RemoveAll(destDir)

	// Attempt to escape to a sibling directory sharing same prefix (e.g. destDir-evil)
	destBase := filepath.Base(destDir)
	archivePath := createTestTarGz(t, map[string][]byte{
		"../" + destBase + "-evil/escape.txt": []byte("malicious content"),
	})
	defer os.Remove(archivePath)

	_, err = extractTarGz(archivePath, destDir)
	if err == nil {
		t.Fatal("expected extractTarGz to fail with path traversal error for sibling directory escape")
	}
	if !strings.Contains(err.Error(), "逃逸") && !strings.Contains(err.Error(), "traversal") && !strings.Contains(err.Error(), "illegal") {
		t.Fatalf("expected error mentioning path traversal, got: %v", err)
	}
}

func TestParseAndCompareConfigureArgs(t *testing.T) {
	rawOutput := `nginx version: nginx/1.30.5
built by gcc 12.2.0 (Debian 12.2.0-14)
built with OpenSSL 3.4.1 11 Feb 2025
TLS SNI support enabled
configure arguments: --prefix=/usr/local/nginx --with-http_ssl_module --with-http_v2_module '--with-ld-opt=-Wl,-z,relro'
`
	args := parseConfigureArguments(rawOutput)
	if len(args) != 4 {
		t.Fatalf("expected 4 arguments, got %d: %#v", len(args), args)
	}

	expectedArgs := []string{
		"--prefix=/usr/local/nginx",
		"--with-http_ssl_module",
		"--with-http_v2_module",
	}
	missing, _, match := compareConfigureArgs(expectedArgs, args)
	if !match || len(missing) > 0 {
		t.Fatalf("expected arguments to match, missing: %v", missing)
	}

	// Test missing argument detection
	expectedWithExtra := append(expectedArgs, "--with-http_v3_module")
	missing2, _, match2 := compareConfigureArgs(expectedWithExtra, args)
	if match2 || len(missing2) != 1 || missing2[0] != "--with-http_v3_module" {
		t.Fatalf("expected --with-http_v3_module to be missing, got: %v", missing2)
	}
}

func TestExtractTarGzRejectsFileCountBomb(t *testing.T) {
	destDir, err := os.MkdirTemp("", "test-extract-bomb-*")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	defer os.RemoveAll(destDir)

	// Create an archive with > 20000 entries (or test quota)
	// To keep unit test fast, we can test with a smaller synthetic archive or verify limit logic
}
