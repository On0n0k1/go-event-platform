package config

import "testing"

func TestLoad(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		t.Setenv("PORT", "")
		t.Setenv("ORDER_SERVICE_URL", "")
		t.Setenv("INVENTORY_SERVICE_URL", "")
		t.Setenv("ANALYTICS_SERVICE_URL", "")

		cfg := Load()

		if cfg.Port != "8080" {
			t.Errorf("Port = %q, want default 8080", cfg.Port)
		}
		if cfg.OrderServiceURL != "http://localhost:8082" {
			t.Errorf("OrderServiceURL = %q, want default", cfg.OrderServiceURL)
		}
		if cfg.InventoryServiceURL != "http://localhost:8081" {
			t.Errorf("InventoryServiceURL = %q, want default", cfg.InventoryServiceURL)
		}
		if cfg.AnalyticsServiceURL != "http://localhost:8084" {
			t.Errorf("AnalyticsServiceURL = %q, want default", cfg.AnalyticsServiceURL)
		}
	})

	t.Run("overrides", func(t *testing.T) {
		t.Setenv("PORT", "9090")
		t.Setenv("ORDER_SERVICE_URL", "http://order-service:8082")
		t.Setenv("INVENTORY_SERVICE_URL", "http://inventory-service:8081")
		t.Setenv("ANALYTICS_SERVICE_URL", "http://analytics-service:8084")

		cfg := Load()

		if cfg.Port != "9090" {
			t.Errorf("Port = %q, want 9090", cfg.Port)
		}
		if cfg.OrderServiceURL != "http://order-service:8082" {
			t.Errorf("OrderServiceURL = %q, want override", cfg.OrderServiceURL)
		}
		if cfg.InventoryServiceURL != "http://inventory-service:8081" {
			t.Errorf("InventoryServiceURL = %q, want override", cfg.InventoryServiceURL)
		}
		if cfg.AnalyticsServiceURL != "http://analytics-service:8084" {
			t.Errorf("AnalyticsServiceURL = %q, want override", cfg.AnalyticsServiceURL)
		}
	})
}
