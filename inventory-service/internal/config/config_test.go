package config

import "testing"

func TestLoad(t *testing.T) {
	t.Run("missing database url", func(t *testing.T) {
		t.Setenv("DATABASE_URL", "")

		if _, err := Load(); err == nil {
			t.Fatal("expected error when DATABASE_URL is unset")
		}
	})

	t.Run("defaults and overrides", func(t *testing.T) {
		t.Setenv("DATABASE_URL", "postgres://example")
		t.Setenv("PORT", "")

		cfg, err := Load()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Port != "8081" {
			t.Errorf("Port = %q, want default 8081", cfg.Port)
		}
		if cfg.DatabaseURL != "postgres://example" {
			t.Errorf("DatabaseURL = %q, want postgres://example", cfg.DatabaseURL)
		}

		t.Setenv("PORT", "9000")
		cfg, err = Load()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Port != "9000" {
			t.Errorf("Port = %q, want 9000", cfg.Port)
		}
	})
}
