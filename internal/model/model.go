package model

import (
	"sync"
	"time"
)

// BuildStatus represents the lifecycle state of an Nginx build job.
type BuildStatus string

const (
	StatusQueued      BuildStatus = "queued"      // 等待编译
	StatusDownloading BuildStatus = "downloading" // 下载源码
	StatusConfiguring BuildStatus = "configuring" // 配置中
	StatusBuilding    BuildStatus = "building"    // 编译中
	StatusPackaging   BuildStatus = "packaging"   // 打包中
	StatusCompleted   BuildStatus = "completed"   // 编译完成
	StatusFailed      BuildStatus = "failed"      // 编译失败
	StatusCancelled   BuildStatus = "cancelled"   // 任务已取消
)

// OptionCategory represents parameter functional grouping.
type OptionCategory string

const (
	CategoryHTTP        OptionCategory = "HTTP"
	CategoryStream      OptionCategory = "Stream"
	CategoryMail        OptionCategory = "Mail"
	CategorySSL         OptionCategory = "SSL/TLS"
	CategoryPerformance OptionCategory = "性能"
	CategoryDebug       OptionCategory = "调试"
	CategoryPaths       OptionCategory = "文件路径"
	CategorySystem      OptionCategory = "系统相关"
	CategoryOther       OptionCategory = "其他官方模块"
)

// NginxOption describes a single official `./configure` parameter.
type NginxOption struct {
	ID             string         `json:"id"`                        // Internal identifier, e.g. "http_ssl"
	Name           string         `json:"name"`                      // Full configure flag, e.g. "--with-http_ssl_module"
	Flag           string         `json:"flag"`                      // Canonical configure flag
	Description    string         `json:"description"`               // Human readable description
	DefaultState   bool           `json:"default_state"`             // true if enabled by default in nginx source
	Category       OptionCategory `json:"category"`                  // Category grouping
	Type           string         `json:"type"`                      // "bool" or "path"
	DefaultValue   string         `json:"default_value,omitempty"`   // for path/string options
	DependsOn      []string       `json:"depends_on,omitempty"`      // option IDs that must be enabled
	ConflictsWith  []string       `json:"conflicts_with,omitempty"`  // option IDs that cannot be enabled together
	RequiresLib    string         `json:"requires_lib,omitempty"`    // e.g. "openssl", "pcre2", "zlib", "libxml2"
	DynamicAllowed bool           `json:"dynamic_allowed,omitempty"` // true if =dynamic is supported
}

// VersionInfo represents Nginx release version details.
type VersionInfo struct {
	Version     string `json:"version"`
	Channel     string `json:"channel"` // "stable", "mainline", "legacy"
	ReleaseDate string `json:"release_date,omitempty"`
	SourceURL   string `json:"source_url"`
	ExpectedSHA string `json:"expected_sha256,omitempty"`
	IsDefault   bool   `json:"is_default"`
}

// DepLibraryInfo represents a preset third-party source library (OpenSSL, PCRE, zlib).
type DepLibraryInfo struct {
	Name        string `json:"name"`         // "openssl", "pcre", "zlib"
	DisplayName string `json:"display_name"` // "OpenSSL 3.4.1", etc.
	Version     string `json:"version"`
	SourceURL   string `json:"source_url"`
	ExpectedSHA string `json:"expected_sha256,omitempty"`
	Description string `json:"description"`
	IsDefault   bool   `json:"is_default"`
}

// ThirdPartySourcesSpec configures third-party source dependencies for static compilation into Nginx.
type ThirdPartySourcesSpec struct {
	UseOpenSSLSource bool   `json:"use_openssl_source"`
	OpenSSLVersion   string `json:"openssl_version,omitempty"` // e.g. "3.4.1" or "custom"
	OpenSSLSourceURL string `json:"openssl_source_url,omitempty"`
	OpenSSLOpt       string `json:"openssl_opt,omitempty"` // options for OpenSSL ./config

	UsePCRESource bool   `json:"use_pcre_source"`
	PCREVersion   string `json:"pcre_version,omitempty"` // e.g. "10.45" or "custom"
	PCRESourceURL string `json:"pcre_source_url,omitempty"`
	PCREOpt       string `json:"pcre_opt,omitempty"`

	UseZlibSource bool   `json:"use_zlib_source"`
	ZlibVersion   string `json:"zlib_version,omitempty"` // e.g. "1.3.1"
	ZlibSourceURL string `json:"zlib_source_url,omitempty"`
	ZlibOpt       string `json:"zlib_opt,omitempty"`
}

// ArtifactInfo holds details of the compiled and packaged Nginx artifact.
type ArtifactInfo struct {
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
	DownloadURL string `json:"download_url"`
	Path        string `json:"path,omitempty"`
}

// VerifyResult stores `nginx -V` verification details after compilation.
type VerifyResult struct {
	Passed         bool     `json:"passed"`
	VersionMatch   bool     `json:"version_match"`
	ArgsMatch      bool     `json:"args_match"`
	VersionText    string   `json:"version_text"`
	RawOutput      string   `json:"raw_output"`
	CheckError     string   `json:"check_error,omitempty"`
	MissingArgs    []string `json:"missing_args,omitempty"`
	UnexpectedArgs []string `json:"unexpected_args,omitempty"`
	SharedLibs     []string `json:"shared_libs,omitempty"`
	SmokeTest      string   `json:"smoke_test,omitempty"`
}

// HostInfo records environment information where the build ran.
type HostInfo struct {
	OS         string `json:"os"`
	Arch       string `json:"arch"`
	Hostname   string `json:"hostname"`
	Kernel     string `json:"kernel"`
	GoVersion  string `json:"go_version"`
	NumCPU     int    `json:"num_cpu"`
	GCCVersion string `json:"gcc_version"`
}

// BuildJob represents a complete Nginx build task with state and audit data.
type BuildJob struct {
	mu                 sync.RWMutex           `json:"-"`
	BuildID            string                 `json:"build_id"`
	NginxVersion       string                 `json:"nginx_version"`
	TargetOS           string                 `json:"target_os"`
	TargetArch         string                 `json:"target_arch"`
	Options            []string               `json:"options"` // Selected option IDs
	PathOverrides      map[string]string      `json:"path_overrides,omitempty"`
	ThirdPartySources  *ThirdPartySourcesSpec `json:"third_party_sources,omitempty"` // OpenSSL, PCRE, zlib
	ConfigureArguments []string               `json:"configure_arguments"`           // Structured ./configure args
	FullConfigureCmd   string                 `json:"full_configure_cmd"`            // Preview ./configure string
	Status             BuildStatus            `json:"status"`
	CurrentStep        string                 `json:"current_step"`
	Progress           int                    `json:"progress"` // 0 - 100 percentage
	StartTime          time.Time              `json:"start_time"`
	EndTime            *time.Time             `json:"end_time,omitempty"`
	DurationSeconds    float64                `json:"duration_seconds"`
	CompilerVersion    string                 `json:"compiler_version"`
	Artifact           *ArtifactInfo          `json:"artifact,omitempty"`
	SourceURL          string                 `json:"source_url"`
	SourceSHA256       string                 `json:"source_sha256"`
	HostInfo           *HostInfo              `json:"host_info,omitempty"`
	VerifyResult       *VerifyResult          `json:"verify_result,omitempty"`
	ErrorMessage       string                 `json:"error_message,omitempty"`
	LogFilePath        string                 `json:"log_file_path,omitempty"`
}

// Clone returns a deep copy of BuildJob to prevent data races when reading state concurrently.
func (j *BuildJob) Clone() *BuildJob {
	if j == nil {
		return nil
	}
	j.mu.RLock()
	defer j.mu.RUnlock()

	clone := &BuildJob{
		BuildID:          j.BuildID,
		NginxVersion:     j.NginxVersion,
		TargetOS:         j.TargetOS,
		TargetArch:       j.TargetArch,
		FullConfigureCmd: j.FullConfigureCmd,
		Status:           j.Status,
		CurrentStep:      j.CurrentStep,
		Progress:         j.Progress,
		StartTime:        j.StartTime,
		DurationSeconds:  j.DurationSeconds,
		CompilerVersion:  j.CompilerVersion,
		SourceURL:        j.SourceURL,
		SourceSHA256:     j.SourceSHA256,
		ErrorMessage:     j.ErrorMessage,
		LogFilePath:      j.LogFilePath,
	}

	if len(j.Options) > 0 {
		clone.Options = make([]string, len(j.Options))
		copy(clone.Options, j.Options)
	}
	if j.PathOverrides != nil {
		clone.PathOverrides = make(map[string]string, len(j.PathOverrides))
		for k, v := range j.PathOverrides {
			clone.PathOverrides[k] = v
		}
	}
	if j.ThirdPartySources != nil {
		tp := *j.ThirdPartySources
		clone.ThirdPartySources = &tp
	}
	if len(j.ConfigureArguments) > 0 {
		clone.ConfigureArguments = make([]string, len(j.ConfigureArguments))
		copy(clone.ConfigureArguments, j.ConfigureArguments)
	}
	if j.EndTime != nil {
		t := *j.EndTime
		clone.EndTime = &t
	}
	if j.Artifact != nil {
		art := *j.Artifact
		clone.Artifact = &art
	}
	if j.HostInfo != nil {
		host := *j.HostInfo
		clone.HostInfo = &host
	}
	if j.VerifyResult != nil {
		v := *j.VerifyResult
		if len(j.VerifyResult.MissingArgs) > 0 {
			v.MissingArgs = make([]string, len(j.VerifyResult.MissingArgs))
			copy(v.MissingArgs, j.VerifyResult.MissingArgs)
		}
		if len(j.VerifyResult.UnexpectedArgs) > 0 {
			v.UnexpectedArgs = make([]string, len(j.VerifyResult.UnexpectedArgs))
			copy(v.UnexpectedArgs, j.VerifyResult.UnexpectedArgs)
		}
		if len(j.VerifyResult.SharedLibs) > 0 {
			v.SharedLibs = make([]string, len(j.VerifyResult.SharedLibs))
			copy(v.SharedLibs, j.VerifyResult.SharedLibs)
		}
		clone.VerifyResult = &v
	}
	return clone
}

// Update executes a thread-safe mutation of the job state.
func (j *BuildJob) Update(fn func(job *BuildJob)) {
	if j == nil {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	fn(j)
}

// CreateBuildRequest is the payload received when creating a new build task.
type CreateBuildRequest struct {
	Version              string                 `json:"version"`                  // e.g. "stable" or "1.30.5"
	TargetOS             string                 `json:"target_os,omitempty"`      // defaults to host OS (linux)
	TargetArch           string                 `json:"target_arch,omitempty"`    // defaults to host arch (amd64 / arm64)
	Options              []string               `json:"options"`                  // list of option IDs
	PathOverrides        map[string]string      `json:"path_overrides,omitempty"` // prefix, conf-path, etc.
	ThirdPartySources    *ThirdPartySourcesSpec `json:"third_party_sources,omitempty"`
	AutoResolveConflicts bool                   `json:"auto_resolve_conflicts,omitempty"`
}

// PreviewRequest is used to preview configure command without starting a build.
type PreviewRequest struct {
	Version           string                 `json:"version,omitempty"`
	Options           []string               `json:"options"`
	PathOverrides     map[string]string      `json:"path_overrides,omitempty"`
	ThirdPartySources *ThirdPartySourcesSpec `json:"third_party_sources,omitempty"`
}

// PreviewResponse contains generated configure arguments and command.
type PreviewResponse struct {
	NginxVersion   string   `json:"nginx_version"`
	ConfigureArgs  []string `json:"configure_args"`
	FullCommand    string   `json:"full_command"`
	ValidationWarn []string `json:"validation_warnings,omitempty"`
	Conflicts      []string `json:"conflicts,omitempty"`
	HasConflicts   bool     `json:"has_conflicts"`
}

// IsTerminal reports states that must not be overwritten by later worker phases.
func IsTerminal(status BuildStatus) bool {
	return status == StatusCompleted || status == StatusFailed || status == StatusCancelled
}
