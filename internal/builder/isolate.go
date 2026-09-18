package builder

import (
	"fmt"
	"os"
	"path/filepath"
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
	base := filepath.Join(buildRootDir, buildID)
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
