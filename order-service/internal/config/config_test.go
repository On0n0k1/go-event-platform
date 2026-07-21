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
		t.Setenv("INVENTORY_SERVICE_URL", "")

		cfg, err := Load()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Port != "8082" {
			t.Errorf("Port = %q, want default 8082", cfg.Port)
		}
		if cfg.InventoryServiceURL != "http://localhost:8081" {
			t.Errorf("InventoryServiceURL = %q, want default http://localhost:8081", cfg.InventoryServiceURL)
		}

		t.Setenv("INVENTORY_SERVICE_URL", "http://inventory-service:8081")
		cfg, err = Load()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.InventoryServiceURL != "http://inventory-service:8081" {
			t.Errorf("InventoryServiceURL = %q, want override", cfg.InventoryServiceURL)
		}
	})
}
