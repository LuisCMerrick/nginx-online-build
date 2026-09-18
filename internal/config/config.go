package config

import (
	"os"
	"path/filepath"
	"strconv"
	"time"
)

type Config struct {
	Port              string
	DataDir           string
	BuildDir          string
	CacheDir          string
	MaxConcurrentJobs int
	JobTimeout        time.Duration
}

func LoadConfig() *Config {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8090"
	}

	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = "./data"
	}

	maxJobs := 2
	if val := os.Getenv("MAX_CONCURRENT_JOBS"); val != "" {
		if n, err := strconv.Atoi(val); err == nil && n > 0 {
			maxJobs = n
		}
	}

	timeoutMinutes := 20
	if val := os.Getenv("JOB_TIMEOUT_MINUTES"); val != "" {
		if n, err := strconv.Atoi(val); err == nil && n > 0 {
			timeoutMinutes = n
		}
	}

	buildDir := filepath.Join(dataDir, "builds")
	cacheDir := filepath.Join(dataDir, "cache")

	_ = os.MkdirAll(buildDir, 0755)
	_ = os.MkdirAll(cacheDir, 0755)

	return &Config{
		Port:              port,
		DataDir:           dataDir,
		BuildDir:          buildDir,
		CacheDir:          cacheDir,
		MaxConcurrentJobs: maxJobs,
		JobTimeout:        time.Duration(timeoutMinutes) * time.Minute,
	}
}
