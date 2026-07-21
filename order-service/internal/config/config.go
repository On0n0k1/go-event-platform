package config

import (
	"fmt"
	"os"
)

type Config struct {
	Port                string
	DatabaseURL         string
	InventoryServiceURL string
	NatsURL             string
}

func Load() (Config, error) {
	cfg := Config{
		Port:                getEnv("PORT", "8082"),
		DatabaseURL:         os.Getenv("DATABASE_URL"),
		InventoryServiceURL: getEnv("INVENTORY_SERVICE_URL", "http://localhost:8081"),
		NatsURL:             getEnv("NATS_URL", "nats://localhost:4222"),
	}

	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
