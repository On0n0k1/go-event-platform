package config

import "os"

type Config struct {
	Port                string
	OrderServiceURL     string
	InventoryServiceURL string
}

func Load() Config {
	return Config{
		Port:                getEnv("PORT", "8080"),
		OrderServiceURL:     getEnv("ORDER_SERVICE_URL", "http://localhost:8082"),
		InventoryServiceURL: getEnv("INVENTORY_SERVICE_URL", "http://localhost:8081"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
