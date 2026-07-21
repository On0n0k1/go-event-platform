package config

import "os"

type Config struct {
	Port    string
	NatsURL string
}

func Load() Config {
	return Config{
		Port:    getEnv("PORT", "8083"),
		NatsURL: getEnv("NATS_URL", "nats://localhost:4222"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
