package config

import (
	"fmt"
	"os"
)

type Config struct {
	Port         string
	GRPCPort     string
	DatabaseURL  string
	RedisAddr    string
	OTLPEndpoint string
	TLSCertFile  string
	TLSKeyFile   string
	TLSCAFile    string
}

func Load() (Config, error) {
	cfg := Config{
		Port:         getEnv("PORT", "8081"),
		GRPCPort:     getEnv("GRPC_PORT", "9081"),
		DatabaseURL:  os.Getenv("DATABASE_URL"),
		RedisAddr:    getEnv("REDIS_ADDR", "localhost:6379"),
		OTLPEndpoint: getEnv("OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4317"),
		TLSCertFile:  getEnv("TLS_CERT_FILE", "/certs/inventory-service.crt"),
		TLSKeyFile:   getEnv("TLS_KEY_FILE", "/certs/inventory-service.key"),
		TLSCAFile:    getEnv("TLS_CA_FILE", "/certs/ca.crt"),
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
