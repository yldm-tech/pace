package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultAddress         = ":8000"
	defaultShutdownTimeout = 10 * time.Second
	defaultMaxOpenConns    = 25
	defaultMaxIdleConns    = 5
)

type Config struct {
	Address         string
	DatabaseURL     string
	CORSOrigins     []string
	ShutdownTimeout time.Duration
	MaxOpenConns    int
	MaxIdleConns    int
}

func Load() (Config, error) {
	config := Config{
		Address:         envOrDefault("PACE_API_ADDRESS", defaultAddress),
		DatabaseURL:     strings.TrimSpace(os.Getenv("DATABASE_URL")),
		CORSOrigins:     csvOrDefault(os.Getenv("CORS_ORIGINS"), []string{"http://localhost:3000", "http://localhost:3001"}),
		ShutdownTimeout: durationOrDefault(os.Getenv("SHUTDOWN_TIMEOUT"), defaultShutdownTimeout),
		MaxOpenConns:    positiveIntOrDefault(os.Getenv("DB_MAX_OPEN_CONNS"), defaultMaxOpenConns),
		MaxIdleConns:    positiveIntOrDefault(os.Getenv("DB_MAX_IDLE_CONNS"), defaultMaxIdleConns),
	}
	if config.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}
	if config.MaxIdleConns > config.MaxOpenConns {
		return Config{}, fmt.Errorf("DB_MAX_IDLE_CONNS cannot exceed DB_MAX_OPEN_CONNS")
	}
	return config, nil
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func csvOrDefault(value string, fallback []string) []string {
	values := make([]string, 0)
	for _, candidate := range strings.Split(value, ",") {
		if candidate = strings.TrimSpace(candidate); candidate != "" {
			values = append(values, candidate)
		}
	}
	if len(values) == 0 {
		return fallback
	}
	return values
}

func durationOrDefault(value string, fallback time.Duration) time.Duration {
	duration, err := time.ParseDuration(strings.TrimSpace(value))
	if err != nil || duration <= 0 {
		return fallback
	}
	return duration
}

func positiveIntOrDefault(value string, fallback int) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
