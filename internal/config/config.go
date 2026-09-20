package config

import (
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// AppVersion represents the current release version of Nginx Online Web Builder.
const AppVersion = "1.0.0"

const (
	DefaultMaxQueuedJobs           = 16
	DefaultMaxRetainedBuilds       = 20
	DefaultMaxDownloadBytes  int64 = 256 << 20
	DefaultMaxCacheBytes     int64 = 1024 << 20
	MaxRequestBytes          int64 = 64 << 10
	MaxLogBytes              int64 = 16 << 20
)

// Config encapsulates server runtime options, filesystem paths, and worker limits.
type Config struct {
	Host              string
	Port              string
	BasePath          string
	DataDir           string
	BuildDir          string
	CacheDir          string
	MaxConcurrentJobs int
	JobTimeout        time.Duration
	AuthEnabled       bool
	AuthKey           string
	AuthKeyGenerated  bool
	CookieSecure      bool
	MaxQueuedJobs     int
	MaxRetainedBuilds int
	MaxDownloadBytes  int64
	MaxCacheBytes     int64
}

// ParseCLI parses command-line flags, environment variables, and fallback defaults.
// Precedence order: CLI flags > Environment variables > Default values.
// All console help and diagnostic outputs are strictly in English.
func ParseCLI(args []string) *Config {
	fs := flag.NewFlagSet("nginx-builder", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	// Custom Usage banner in English
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Nginx Online Web Builder v%s\n\n", AppVersion)
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  nginx-builder [options]\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		fmt.Fprintf(os.Stderr, "  -h, --host <ip>        Server listen host/address (default: \"0.0.0.0\", env: HOST)\n")
		fmt.Fprintf(os.Stderr, "  -p, --port <port>      Server listen port (default: \"8090\", env: PORT)\n")
		fmt.Fprintf(os.Stderr, "  -b, --base-path <path> URL base path prefix for reverse proxy (default: \"\", env: BASE_PATH)\n")
		fmt.Fprintf(os.Stderr, "  -a, --auth             Enable URL access authentication (default: true, disable with --no-auth, env: AUTH_ENABLED)\n")
		fmt.Fprintf(os.Stderr, "      --no-auth          Explicitly disable access authentication\n")
		fmt.Fprintf(os.Stderr, "  -k, --auth-key <key>   Custom secret key for URL access authentication (default: auto-generated, env: AUTH_KEY)\n")
		fmt.Fprintf(os.Stderr, "  -d, --data-dir <path>  Working data directory for builds and cache (default: \"./data\", env: DATA_DIR)\n")
		fmt.Fprintf(os.Stderr, "  -j, --jobs <n>         Max concurrent compilation jobs (default: 2, env: MAX_CONCURRENT_JOBS)\n")
		fmt.Fprintf(os.Stderr, "  -t, --timeout <min>    Job execution timeout in minutes (default: 20, env: JOB_TIMEOUT_MINUTES)\n")
		fmt.Fprintf(os.Stderr, "  -v, --version          Display version information and exit\n")
		fmt.Fprintf(os.Stderr, "      --cookie-secure       Require HTTPS for browser session cookies (env: COOKIE_SECURE)\n")
		fmt.Fprintf(os.Stderr, "      --queue-limit <n>     Maximum waiting jobs (default: 16, env: MAX_QUEUED_JOBS)\n")
		fmt.Fprintf(os.Stderr, "      --retain-builds <n>   Terminal job records to retain (default: 20, env: MAX_RETAINED_BUILDS)\n")
		fmt.Fprintf(os.Stderr, "      --max-download-mb <n> Source archive limit in MiB (default: 256, env: MAX_DOWNLOAD_MB)\n")
		fmt.Fprintf(os.Stderr, "      --max-cache-mb <n>    Source cache limit in MiB (default: 1024, env: MAX_CACHE_MB)\n")
		fmt.Fprintf(os.Stderr, "      --help             Display this help message and exit\n\n")
	}

	// Read environment variables as baseline defaults
	envHost := os.Getenv("HOST")
	if envHost == "" {
		envHost = "0.0.0.0"
	}

	envPort := os.Getenv("PORT")
	if envPort == "" {
		envPort = "8090"
	}

	envBasePath := CleanBasePath(os.Getenv("BASE_PATH"))

	envAuth := true
	if val := os.Getenv("AUTH_ENABLED"); val != "" {
		val = strings.ToLower(strings.TrimSpace(val))
		if val == "false" || val == "0" || val == "no" || val == "off" {
			envAuth = false
		}
	} else if val := os.Getenv("AUTH"); val != "" {
		val = strings.ToLower(strings.TrimSpace(val))
		if val == "false" || val == "0" || val == "no" || val == "off" {
			envAuth = false
		}
	}
	envAuthKey := strings.TrimSpace(os.Getenv("AUTH_KEY"))

	envDataDir := os.Getenv("DATA_DIR")
	if envDataDir == "" {
		envDataDir = "./data"
	}

	envJobs := 2
	if val := os.Getenv("MAX_CONCURRENT_JOBS"); val != "" {
		if n, err := strconv.Atoi(val); err == nil && n > 0 {
			envJobs = n
		}
	}

	envTimeout := 20
	if val := os.Getenv("JOB_TIMEOUT_MINUTES"); val != "" {
		if n, err := strconv.Atoi(val); err == nil && n > 0 {
			envTimeout = n
		}
	}

	var (
		hostFlag     string
		portFlag     string
		basePathFlag string
		authFlag     bool
		noAuthFlag   bool
		authKeyFlag  string
		dataDirFlag  string
		jobsFlag     int
		timeoutFlag  int
		versionFlag  bool
		showHelpFlag bool
	)

	queueLimit := fs.Int("queue-limit", positiveEnv("MAX_QUEUED_JOBS", DefaultMaxQueuedJobs), "Maximum waiting jobs")
	retainBuilds := fs.Int("retain-builds", positiveEnv("MAX_RETAINED_BUILDS", DefaultMaxRetainedBuilds), "Completed job records to retain")
	downloadMB := fs.Int("max-download-mb", positiveEnv("MAX_DOWNLOAD_MB", int(DefaultMaxDownloadBytes>>20)), "Maximum source archive MiB")
	cacheMB := fs.Int("max-cache-mb", positiveEnv("MAX_CACHE_MB", int(DefaultMaxCacheBytes>>20)), "Maximum cached archive MiB")
	cookieSecure := fs.Bool("cookie-secure", strings.EqualFold(os.Getenv("COOKIE_SECURE"), "true") || os.Getenv("COOKIE_SECURE") == "1", "Secure cookies behind an HTTPS reverse proxy")

	fs.StringVar(&hostFlag, "host", envHost, "Listen address")
	fs.StringVar(&hostFlag, "h", envHost, "Listen address (short)")

	fs.StringVar(&portFlag, "port", envPort, "Listen port")
	fs.StringVar(&portFlag, "p", envPort, "Listen port (short)")

	fs.StringVar(&basePathFlag, "base-path", envBasePath, "Base URL subpath prefix")
	fs.StringVar(&basePathFlag, "b", envBasePath, "Base URL subpath prefix (short)")

	fs.BoolVar(&authFlag, "auth", envAuth, "Enable URL access authentication (default: true)")
	fs.BoolVar(&authFlag, "a", envAuth, "Enable URL access authentication (short)")
	fs.BoolVar(&noAuthFlag, "no-auth", false, "Explicitly disable URL access authentication")

	fs.StringVar(&authKeyFlag, "auth-key", envAuthKey, "Custom access authentication key")
	fs.StringVar(&authKeyFlag, "k", envAuthKey, "Custom access authentication key (short)")

	fs.StringVar(&dataDirFlag, "data-dir", envDataDir, "Data directory")
	fs.StringVar(&dataDirFlag, "d", envDataDir, "Data directory (short)")

	fs.IntVar(&jobsFlag, "jobs", envJobs, "Max concurrent jobs")
	fs.IntVar(&jobsFlag, "j", envJobs, "Max concurrent jobs (short)")

	fs.IntVar(&timeoutFlag, "timeout", envTimeout, "Job timeout in minutes")
	fs.IntVar(&timeoutFlag, "t", envTimeout, "Job timeout in minutes (short)")

	fs.BoolVar(&versionFlag, "version", false, "Print version")
	fs.BoolVar(&versionFlag, "v", false, "Print version (short)")

	fs.BoolVar(&showHelpFlag, "help", false, "Show help")

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "Error parsing arguments: %v\n", err)
		os.Exit(2)
	}

	if showHelpFlag {
		fs.Usage()
		os.Exit(0)
	}

	if versionFlag {
		fmt.Printf("nginx-builder v%s\n", AppVersion)
		os.Exit(0)
	}

	if jobsFlag <= 0 {
		jobsFlag = 2
	}
	if timeoutFlag <= 0 {
		timeoutFlag = 20
	}

	authEnabled := authFlag
	if noAuthFlag {
		authEnabled = false
	}

	if *queueLimit <= 0 || *retainBuilds <= 0 || *downloadMB <= 0 || *cacheMB < *downloadMB || *cacheMB > 1048576 {
		fmt.Fprintln(os.Stderr, "Invalid resource limits: positive values required, cache >= download, cache <= 1048576 MiB")
		os.Exit(2)
	}
	authKey := strings.TrimSpace(authKeyFlag)
	authKeyGenerated := authEnabled && authKey == ""
	if authEnabled && authKey == "" {
		// Import will be handled or fallback token
		authKey = GenerateDefaultAuthKey()
	}

	absData, err := PrepareDataDir(dataDirFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot prepare data directory: %v\n", err)
		os.Exit(2)
	}
	buildDir := filepath.Join(absData, "builds")
	cacheDir := filepath.Join(absData, "cache")

	return &Config{
		Host:              hostFlag,
		Port:              portFlag,
		BasePath:          CleanBasePath(basePathFlag),
		DataDir:           absData,
		BuildDir:          buildDir,
		CacheDir:          cacheDir,
		MaxConcurrentJobs: jobsFlag,
		JobTimeout:        time.Duration(timeoutFlag) * time.Minute,
		AuthEnabled:       authEnabled,
		AuthKey:           authKey,
		AuthKeyGenerated:  authKeyGenerated,
		CookieSecure:      *cookieSecure,
		MaxQueuedJobs:     *queueLimit,
		MaxRetainedBuilds: *retainBuilds,
		MaxDownloadBytes:  int64(*downloadMB) << 20,
		MaxCacheBytes:     int64(*cacheMB) << 20,
	}
}

// GenerateDefaultAuthKey creates a secure random 32-character hex key if none provided.
func GenerateDefaultAuthKey() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err == nil {
		return hex.EncodeToString(b)
	}
	panic("cryptographic random source unavailable")
}

// CleanBasePath normalizes URL subpath prefixes (e.g. "/nginx" or "" for root).
func CleanBasePath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" || p == "/" || p == "\\" {
		return ""
	}
	p = strings.ReplaceAll(p, "\\", "/")
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	p = strings.TrimRight(p, "/")
	return p
}

// LoadConfig maintains backward-compatibility by invoking ParseCLI with os.Args[1:].
func LoadConfig() *Config {
	return ParseCLI(os.Args[1:])
}

func positiveEnv(name string, fallback int) int {
	if value, err := strconv.Atoi(os.Getenv(name)); err == nil && value > 0 {
		return value
	}
	return fallback
}

// PrepareDataDir resolves paths before any child process changes its working directory.
func PrepareDataDir(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	for _, dir := range []string{absolute, filepath.Join(absolute, "builds"), filepath.Join(absolute, "cache")} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return "", err
		}
		probe, err := os.CreateTemp(dir, ".write-check-*")
		if err != nil {
			return "", err
		}
		name := probe.Name()
		err = probe.Close()
		removeErr := os.Remove(name)
		if err != nil {
			return "", err
		}
		if removeErr != nil {
			return "", removeErr
		}
	}
	return absolute, nil
}
