package builder

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Workspace defines isolated filesystem paths for a single build job.
type Workspace struct {
	BaseDir      string
	SourceDir    string
	DepsDir      string
	WorkDir      string
	LogsDir      string
	ArtifactsDir string

	BuildLogPath     string
	ConfigureLogPath string
	ErrorLogPath     string
	MetadataPath     string
}

// NewWorkspace initializes and creates isolated directories for a given build ID.
func NewWorkspace(buildRootDir, buildID string) (*Workspace, error) {
	if !validBuildID(buildID) {
		return nil, fmt.Errorf("invalid build ID")
	}
	root, err := filepath.Abs(buildRootDir)
	if err != nil {
		return nil, err
	}
	base := filepath.Join(root, buildID)
	source := filepath.Join(base, "source")
	deps := filepath.Join(base, "deps")
	work := filepath.Join(base, "work")
	logs := filepath.Join(base, "logs")
	artifacts := filepath.Join(base, "artifacts")

	dirs := []string{source, deps, work, logs, artifacts}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return nil, fmt.Errorf("创建隔离工作目录失败 %s: %w", d, err)
		}
	}

	return &Workspace{
		BaseDir:          base,
		SourceDir:        source,
		DepsDir:          deps,
		WorkDir:          work,
		LogsDir:          logs,
		ArtifactsDir:     artifacts,
		BuildLogPath:     filepath.Join(logs, "build.log"),
		ConfigureLogPath: filepath.Join(logs, "configure.log"),
		ErrorLogPath:     filepath.Join(logs, "error.log"),
		MetadataPath:     filepath.Join(base, "meta.json"),
	}, nil
}

func validBuildID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

func safeArtifactPath(dir, name string) (string, error) {
	if name == "" || name == "." || strings.ContainsAny(name, "/\\") || filepath.Base(name) != name {
		return "", fmt.Errorf("invalid artifact filename")
	}
	path := filepath.Join(dir, name)
	rel, err := filepath.Rel(dir, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("artifact path escapes its directory")
	}
	return path, nil
}
