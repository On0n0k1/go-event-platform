package config

import (
	"fmt"
	"os"
)

type Config struct {
	Port        string
	GRPCPort    string
	DatabaseURL string
	RedisAddr   string
}

func Load() (Config, error) {
	cfg := Config{
		Port:        getEnv("PORT", "8081"),
		GRPCPort:    getEnv("GRPC_PORT", "9081"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
		RedisAddr:   getEnv("REDIS_ADDR", "localhost:6379"),
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
