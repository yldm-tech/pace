// Package live serves the collaborative editor: the websocket every open page is connected to, and the few HTTP routes beside it.
//
// It is a separate process from the API rather than a package inside it, because a page is only consistent if every client editing it is talking to the same set of servers, and because it holds a document in memory for as long as somebody has it open.
package live

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// Config is the live service's environment, with the same names, the same defaults and the same idea of what is required as the service it replaces. A missing or malformed value is fatal at startup rather than at the first request.
type Config struct {
	AppVersion           string
	Hostname             string
	Port                 string
	APIBaseURL           string
	CORSAllowedOrigins   []string
	BasePath             string
	CompressionLevel     int
	CompressionThreshold int
	SecretKey            string
	RedisHost            string
	RedisPort            int
	RedisURL             string
}

// LoadConfig reads the process environment. The two rules worth stating: API_BASE_URL has to be a url and LIVE_SERVER_SECRET_KEY has to be set, and nothing else is required.
func LoadConfig() (Config, error) {
	return loadConfig(os.Getenv)
}

func loadConfig(lookup func(string) string) (Config, error) {
	envOrDefault := func(name, fallback string) string {
		if value := lookup(name); value != "" {
			return value
		}
		return fallback
	}
	numberOrDefault := func(name, fallback string) (int, error) {
		value, err := strconv.Atoi(envOrDefault(name, fallback))
		if err != nil {
			return 0, fmt.Errorf("%s must be a number", name)
		}
		return value, nil
	}

	config := Config{
		AppVersion:         envOrDefault("APP_VERSION", "1.0.0"),
		Hostname:           lookup("HOSTNAME"),
		Port:               envOrDefault("PORT", "3000"),
		APIBaseURL:         lookup("API_BASE_URL"),
		CORSAllowedOrigins: splitOrigins(lookup("CORS_ALLOWED_ORIGINS")),
		BasePath:           envOrDefault("LIVE_BASE_PATH", "/live"),
		SecretKey:          lookup("LIVE_SERVER_SECRET_KEY"),
		RedisHost:          lookup("REDIS_HOST"),
		RedisURL:           lookup("REDIS_URL"),
	}

	var problems []string
	if !isURL(config.APIBaseURL) {
		problems = append(problems, "API_BASE_URL must be a valid URL")
	}
	if config.SecretKey == "" {
		problems = append(problems, "LIVE_SERVER_SECRET_KEY is required")
	}

	level, err := numberOrDefault("COMPRESSION_LEVEL", "6")
	if err != nil {
		problems = append(problems, err.Error())
	}
	config.CompressionLevel = level

	threshold, err := numberOrDefault("COMPRESSION_THRESHOLD", "5000")
	if err != nil {
		problems = append(problems, err.Error())
	}
	config.CompressionThreshold = threshold

	port, err := numberOrDefault("REDIS_PORT", "6379")
	if err != nil {
		problems = append(problems, err.Error())
	}
	config.RedisPort = port

	if len(problems) > 0 {
		return Config{}, fmt.Errorf("invalid environment variables: %s", strings.Join(problems, "; "))
	}
	return config, nil
}

// RedisAddress is the address the relay connects to, empty when neither a url nor a host is configured — in which case the service runs as a single node and a page open on two servers does not converge.
func (c Config) RedisAddress() string {
	if c.RedisURL != "" {
		return c.RedisURL
	}
	if c.RedisHost != "" {
		return fmt.Sprintf("redis://%s:%d", c.RedisHost, c.RedisPort)
	}
	return ""
}

// splitOrigins keeps the empty entry an empty setting produces, because the service it replaces does too: an empty CORS_ALLOWED_ORIGINS becomes a one-element list holding the empty string, and the browser preflight then matches nothing.
func splitOrigins(value string) []string {
	parts := strings.Split(value, ",")
	for i, part := range parts {
		parts[i] = strings.TrimSpace(part)
	}
	return parts
}

// isURL is what zod's .url() accepts, which is what the JavaScript URL constructor accepts: anything carrying a scheme. It is looser than "an http address" on purpose, because the setting it validates is validated that loosely.
func isURL(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Scheme != "" && raw != ""
}
