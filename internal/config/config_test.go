package config_test

import (
	"nginx-builder/internal/config"
	"os"
	"testing"
)

func TestParseCLI_AuthDefaultsAndFlags(t *testing.T) {
	t.Run("Default configuration enables auth and generates random key", func(t *testing.T) {
		cfg := config.ParseCLI([]string{})
		if !cfg.AuthEnabled {
			t.Fatalf("expected AuthEnabled to be true by default, got false")
		}
		if len(cfg.AuthKey) != 32 {
			t.Fatalf("expected 32-character hex AuthKey, got %s (len %d)", cfg.AuthKey, len(cfg.AuthKey))
		}
	})

	t.Run("Explicitly disable auth with --no-auth", func(t *testing.T) {
		cfg := config.ParseCLI([]string{"--no-auth"})
		if cfg.AuthEnabled {
			t.Fatalf("expected AuthEnabled to be false when --no-auth is passed")
		}
	})

	t.Run("Explicitly disable auth with -auth=false", func(t *testing.T) {
		cfg := config.ParseCLI([]string{"-auth=false"})
		if cfg.AuthEnabled {
			t.Fatalf("expected AuthEnabled to be false when -auth=false is passed")
		}
	})

	t.Run("Custom key via --auth-key flag", func(t *testing.T) {
		custom := "custom_secret_password_xyz"
		cfg := config.ParseCLI([]string{"--auth-key", custom})
		if !cfg.AuthEnabled {
			t.Fatalf("expected AuthEnabled to be true")
		}
		if cfg.AuthKey != custom {
			t.Fatalf("expected AuthKey %s, got %s", custom, cfg.AuthKey)
		}
	})

	t.Run("Disable auth via AUTH_ENABLED environment variable", func(t *testing.T) {
		os.Setenv("AUTH_ENABLED", "false")
		defer os.Unsetenv("AUTH_ENABLED")

		cfg := config.ParseCLI([]string{})
		if cfg.AuthEnabled {
			t.Fatalf("expected AuthEnabled to be false when AUTH_ENABLED=false")
		}
	})
}
