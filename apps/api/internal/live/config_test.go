package live

import (
	"strings"
	"testing"
)

func fromMap(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

func validEnvironment() map[string]string {
	return map[string]string{"API_BASE_URL": "http://api.test", "LIVE_SERVER_SECRET_KEY": "secret"}
}

func TestConfigDefaults(t *testing.T) {
	config, err := loadConfig(fromMap(validEnvironment()))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if config.AppVersion != "1.0.0" {
		t.Errorf("app version = %q", config.AppVersion)
	}
	if config.Port != "3000" {
		t.Errorf("port = %q", config.Port)
	}
	if config.BasePath != "/live" {
		t.Errorf("base path = %q", config.BasePath)
	}
	if config.CompressionLevel != 6 || config.CompressionThreshold != 5000 {
		t.Errorf("compression = %d/%d", config.CompressionLevel, config.CompressionThreshold)
	}
	if config.RedisPort != 6379 {
		t.Errorf("redis port = %d", config.RedisPort)
	}
	// An unset list is one empty entry rather than no entries, which is what splitting an empty string on commas gives.
	if len(config.CORSAllowedOrigins) != 1 || config.CORSAllowedOrigins[0] != "" {
		t.Errorf("cors origins = %#v", config.CORSAllowedOrigins)
	}
}

func TestConfigRequiresAnAPIURLAndASecret(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		env     map[string]string
		wantIn  string
		wantErr bool
	}{
		{name: "no api url", env: map[string]string{"LIVE_SERVER_SECRET_KEY": "secret"}, wantIn: "API_BASE_URL", wantErr: true},
		{name: "api url is not a url", env: map[string]string{"API_BASE_URL": "not a url", "LIVE_SERVER_SECRET_KEY": "secret"}, wantIn: "API_BASE_URL", wantErr: true},
		{name: "no secret", env: map[string]string{"API_BASE_URL": "http://api.test"}, wantIn: "LIVE_SERVER_SECRET_KEY", wantErr: true},
		{name: "compression level is not a number", env: merge(validEnvironment(), "COMPRESSION_LEVEL", "loud"), wantIn: "COMPRESSION_LEVEL", wantErr: true},
		// A url with any scheme is accepted, because the validation it replaces accepts any scheme.
		{name: "any scheme is a url", env: map[string]string{"API_BASE_URL": "ftp://api.test", "LIVE_SERVER_SECRET_KEY": "secret"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := loadConfig(fromMap(testCase.env))
			if testCase.wantErr {
				if err == nil {
					t.Fatal("no error, want one")
				}
				if !strings.Contains(err.Error(), testCase.wantIn) {
					t.Errorf("err = %v, want it to name %s", err, testCase.wantIn)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v, want none", err)
			}
		})
	}
}

// TestConfigReportsEveryProblemAtOnce matters because the service exits on the first load: a deployment with two things wrong should learn both.
func TestConfigReportsEveryProblemAtOnce(t *testing.T) {
	_, err := loadConfig(fromMap(map[string]string{"COMPRESSION_LEVEL": "loud"}))
	if err == nil {
		t.Fatal("no error, want one")
	}
	for _, name := range []string{"API_BASE_URL", "LIVE_SERVER_SECRET_KEY", "COMPRESSION_LEVEL"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("err = %v, want it to name %s", err, name)
		}
	}
}

func TestRedisAddress(t *testing.T) {
	for _, testCase := range []struct {
		name string
		env  map[string]string
		want string
	}{
		{name: "a url wins", env: merge(merge(validEnvironment(), "REDIS_URL", "redis://somewhere:6380"), "REDIS_HOST", "ignored"), want: "redis://somewhere:6380"},
		{name: "a host and a port", env: merge(validEnvironment(), "REDIS_HOST", "cache"), want: "redis://cache:6379"},
		{name: "a host and a chosen port", env: merge(merge(validEnvironment(), "REDIS_HOST", "cache"), "REDIS_PORT", "6390"), want: "redis://cache:6390"},
		{name: "neither", env: validEnvironment(), want: ""},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			config, err := loadConfig(fromMap(testCase.env))
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			if got := config.RedisAddress(); got != testCase.want {
				t.Errorf("address = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestOriginsAreTrimmed(t *testing.T) {
	config, err := loadConfig(fromMap(merge(validEnvironment(), "CORS_ALLOWED_ORIGINS", " https://a.test , https://b.test ")))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(config.CORSAllowedOrigins) != 2 || config.CORSAllowedOrigins[0] != "https://a.test" || config.CORSAllowedOrigins[1] != "https://b.test" {
		t.Errorf("origins = %#v", config.CORSAllowedOrigins)
	}
}

func merge(base map[string]string, name, value string) map[string]string {
	out := make(map[string]string, len(base)+1)
	for key, existing := range base {
		out[key] = existing
	}
	out[name] = value
	return out
}
