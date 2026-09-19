package builder

import "testing"

func TestArtifactDownloadPathIsRootRelative(t *testing.T) {
	if got, want := artifactDownloadPath("build-123"), "/api/builds/build-123/artifact"; got != want {
		t.Fatalf("artifactDownloadPath() = %q, want %q", got, want)
	}
}
