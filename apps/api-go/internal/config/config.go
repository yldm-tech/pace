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
	Auth            AuthConfig
}

type AuthConfig struct {
	SecretKey               string
	SecretKeyFallbacks      []string
	RedisURL                string
	AMQPURL                 string
	WebURL                  string
	AppBaseURL              string
	SpaceBaseURL            string
	SpaceBasePath           string
	SessionCookieName       string
	SessionCookieDomain     string
	SessionCookieSecure     bool
	SessionCookieAge        time.Duration
	SessionSaveEveryRequest bool
	CSRFCookieName          string
	CSRFCookieDomain        string
	CSRFCookieSecure        bool
	CSRFCookieAge           time.Duration
	CSRFTrustedOrigins      []string
	AuthenticationRateLimit string
	SkipEnvironmentConfig   bool
	AWSAccessKeyID          string
	AWSSecretAccessKey      string
	AWSRegion               string
	AWSBucketName           string
	AWSEndpointURL          string
	UseMinio                bool
	MinioEndpointSSL        bool
	FileSizeLimit           int64
}

func Load() (Config, error) {
	corsOrigins := csvOrDefault(
		firstNonEmpty(os.Getenv("CORS_ALLOWED_ORIGINS"), os.Getenv("CORS_ORIGINS")),
		[]string{"http://localhost:3000", "http://localhost:3001"},
	)
	trustedOrigins := csvOrDefault(os.Getenv("CSRF_TRUSTED_ORIGINS"), corsOrigins)
	secureCookies := originsRequireSecureCookies(corsOrigins)
	config := Config{
		Address:         envOrDefault("PACE_API_ADDRESS", defaultAddress),
		DatabaseURL:     strings.TrimSpace(os.Getenv("DATABASE_URL")),
		CORSOrigins:     corsOrigins,
		ShutdownTimeout: durationOrDefault(os.Getenv("SHUTDOWN_TIMEOUT"), defaultShutdownTimeout),
		MaxOpenConns:    positiveIntOrDefault(os.Getenv("DB_MAX_OPEN_CONNS"), defaultMaxOpenConns),
		MaxIdleConns:    positiveIntOrDefault(os.Getenv("DB_MAX_IDLE_CONNS"), defaultMaxIdleConns),
		Auth: AuthConfig{
			SecretKey:               strings.TrimSpace(os.Getenv("SECRET_KEY")),
			SecretKeyFallbacks:      csvOrDefault(os.Getenv("SECRET_KEY_FALLBACKS"), nil),
			RedisURL:                strings.TrimSpace(os.Getenv("REDIS_URL")),
			AMQPURL:                 celeryBrokerURL(),
			WebURL:                  strings.TrimRight(envOrDefault("WEB_URL", "http://localhost:8000"), "/"),
			AppBaseURL:              strings.TrimRight(envOrDefault("APP_BASE_URL", "http://localhost:3000"), "/"),
			SpaceBaseURL:            strings.TrimRight(envOrDefault("SPACE_BASE_URL", "http://localhost:3002"), "/"),
			SpaceBasePath:           normalizedBasePath(envOrDefault("SPACE_BASE_PATH", "/spaces/")),
			SessionCookieName:       envOrDefault("SESSION_COOKIE_NAME", "session-id"),
			SessionCookieDomain:     strings.TrimSpace(os.Getenv("COOKIE_DOMAIN")),
			SessionCookieSecure:     secureCookies,
			SessionCookieAge:        secondsOrDefault(os.Getenv("SESSION_COOKIE_AGE"), 7*24*time.Hour),
			SessionSaveEveryRequest: boolOrDefault(os.Getenv("SESSION_SAVE_EVERY_REQUEST"), false),
			CSRFCookieName:          envOrDefault("CSRF_COOKIE_NAME", "csrftoken"),
			CSRFCookieDomain:        strings.TrimSpace(os.Getenv("COOKIE_DOMAIN")),
			CSRFCookieSecure:        secureCookies,
			CSRFCookieAge:           secondsOrDefault(os.Getenv("CSRF_COOKIE_AGE"), 364*24*time.Hour),
			CSRFTrustedOrigins:      trustedOrigins,
			AuthenticationRateLimit: envOrDefault("AUTHENTICATION_RATE_LIMIT", "10/minute"),
			SkipEnvironmentConfig:   boolOrDefault(os.Getenv("SKIP_ENV_VAR"), true),
			AWSAccessKeyID:          strings.TrimSpace(os.Getenv("AWS_ACCESS_KEY_ID")),
			AWSSecretAccessKey:      strings.TrimSpace(os.Getenv("AWS_SECRET_ACCESS_KEY")),
			AWSRegion:               strings.TrimSpace(os.Getenv("AWS_REGION")),
			AWSBucketName:           envOrDefault("AWS_S3_BUCKET_NAME", "uploads"),
			AWSEndpointURL:          firstNonEmpty(os.Getenv("AWS_S3_ENDPOINT_URL"), os.Getenv("MINIO_ENDPOINT_URL")),
			UseMinio:                boolOrDefault(os.Getenv("USE_MINIO"), false),
			MinioEndpointSSL:        boolOrDefault(os.Getenv("MINIO_ENDPOINT_SSL"), false),
			FileSizeLimit:           int64(positiveIntOrDefault(os.Getenv("FILE_SIZE_LIMIT"), 5*1024*1024)),
		},
	}
	if config.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}
	if config.MaxIdleConns > config.MaxOpenConns {
		return Config{}, fmt.Errorf("DB_MAX_IDLE_CONNS cannot exceed DB_MAX_OPEN_CONNS")
	}
	if config.Auth.SecretKey == "" {
		return Config{}, fmt.Errorf("SECRET_KEY is required")
	}
	return config, nil
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func originsRequireSecureCookies(origins []string) bool {
	if len(origins) == 0 {
		return false
	}
	for _, origin := range origins {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(origin)), "http:") {
			return false
		}
	}
	return true
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

func secondsOrDefault(value string, fallback time.Duration) time.Duration {
	seconds, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || seconds <= 0 {
		return fallback
	}
	return time.Duration(seconds) * time.Second
}

func boolOrDefault(value string, fallback bool) bool {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return fallback
	}
	switch value {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func normalizedBasePath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "/spaces/"
	}
	if !strings.HasPrefix(value, "/") {
		value = "/" + value
	}
	if !strings.HasSuffix(value, "/") {
		value += "/"
	}
	return value
}

func celeryBrokerURL() string {
	if configured := strings.TrimSpace(os.Getenv("AMQP_URL")); configured != "" {
		return configured
	}
	user := envOrDefault("RABBITMQ_USER", "guest")
	password := envOrDefault("RABBITMQ_PASSWORD", "guest")
	host := envOrDefault("RABBITMQ_HOST", "localhost")
	port := envOrDefault("RABBITMQ_PORT", "5672")
	vhost := envOrDefault("RABBITMQ_VHOST", "/")
	return fmt.Sprintf("amqp://%s:%s@%s:%s/%s", user, password, host, port, vhost)
}
