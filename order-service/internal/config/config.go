package config

import (
	"fmt"
	"os"
)

type Config struct {
	Port                     string
	DatabaseURL              string
	InventoryServiceGRPCAddr string
	NatsURL                  string
	OTLPEndpoint             string
}

func Load() (Config, error) {
	cfg := Config{
		Port:                     getEnv("PORT", "8082"),
		DatabaseURL:              os.Getenv("DATABASE_URL"),
		InventoryServiceGRPCAddr: getEnv("INVENTORY_SERVICE_GRPC_ADDR", "localhost:9081"),
		NatsURL:                  getEnv("NATS_URL", "nats://localhost:4222"),
		OTLPEndpoint:             getEnv("OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4317"),
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
