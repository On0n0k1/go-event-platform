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
		t.Setenv("INVENTORY_SERVICE_GRPC_ADDR", "")

		cfg, err := Load()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Port != "8082" {
			t.Errorf("Port = %q, want default 8082", cfg.Port)
		}
		if cfg.InventoryServiceGRPCAddr != "localhost:9081" {
			t.Errorf("InventoryServiceGRPCAddr = %q, want default localhost:9081", cfg.InventoryServiceGRPCAddr)
		}

		t.Setenv("INVENTORY_SERVICE_GRPC_ADDR", "inventory-service:9081")
		cfg, err = Load()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.InventoryServiceGRPCAddr != "inventory-service:9081" {
			t.Errorf("InventoryServiceGRPCAddr = %q, want override", cfg.InventoryServiceGRPCAddr)
		}
	})
}
