package config

import (
	"reflect"
	"testing"
	"time"
)

func TestLoadRequiresDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("SECRET_KEY", "test-secret")
	if _, err := Load(); err == nil {
		t.Fatal("Load() should reject a missing DATABASE_URL")
	}
}

func TestLoadDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://pace:secret@db:5432/pace")
	t.Setenv("SECRET_KEY", "test-secret")
	t.Setenv("PACE_API_ADDRESS", "")
	t.Setenv("CORS_ORIGINS", "")
	t.Setenv("CORS_ALLOWED_ORIGINS", "")
	t.Setenv("SHUTDOWN_TIMEOUT", "")
	t.Setenv("DB_MAX_OPEN_CONNS", "")
	t.Setenv("DB_MAX_IDLE_CONNS", "")

	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Address != ":8000" || got.ShutdownTimeout != 10*time.Second {
		t.Fatalf("unexpected defaults: %#v", got)
	}
	if got.MaxOpenConns != 25 || got.MaxIdleConns != 5 {
		t.Fatalf("unexpected pool defaults: %#v", got)
	}
	wantOrigins := []string{"http://localhost:3000", "http://localhost:3001"}
	if !reflect.DeepEqual(got.CORSOrigins, wantOrigins) {
		t.Fatalf("CORSOrigins = %#v, want %#v", got.CORSOrigins, wantOrigins)
	}
}

func TestLoadOverrides(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://pace:secret@db:5432/pace")
	t.Setenv("SECRET_KEY", "test-secret")
	t.Setenv("PACE_API_ADDRESS", "127.0.0.1:9000")
	t.Setenv("CORS_ORIGINS", "https://pace.example, https://admin.pace.example")
	t.Setenv("CORS_ALLOWED_ORIGINS", "")
	t.Setenv("SHUTDOWN_TIMEOUT", "25s")
	t.Setenv("DB_MAX_OPEN_CONNS", "40")
	t.Setenv("DB_MAX_IDLE_CONNS", "10")

	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Address != "127.0.0.1:9000" || got.ShutdownTimeout != 25*time.Second {
		t.Fatalf("unexpected overrides: %#v", got)
	}
	if got.MaxOpenConns != 40 || got.MaxIdleConns != 10 {
		t.Fatalf("unexpected pool overrides: %#v", got)
	}
	wantOrigins := []string{"https://pace.example", "https://admin.pace.example"}
	if !reflect.DeepEqual(got.CORSOrigins, wantOrigins) {
		t.Fatalf("CORSOrigins = %#v, want %#v", got.CORSOrigins, wantOrigins)
	}
	if !got.Auth.SessionCookieSecure || !got.Auth.CSRFCookieSecure {
		t.Fatal("HTTPS-only CORS origins should produce secure cookies like Django")
	}
}

func TestDjangoCORSVariableTakesPrecedence(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://pace:secret@db:5432/pace")
	t.Setenv("SECRET_KEY", "test-secret")
	t.Setenv("CORS_ALLOWED_ORIGINS", "http://web.pace.test,https://admin.pace.test")
	t.Setenv("CORS_ORIGINS", "https://legacy-name.pace.test")
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"http://web.pace.test", "https://admin.pace.test"}
	if !reflect.DeepEqual(got.CORSOrigins, want) {
		t.Fatalf("CORS origins = %#v", got.CORSOrigins)
	}
	if got.Auth.SessionCookieSecure || got.Auth.CSRFCookieSecure {
		t.Fatal("any HTTP origin should disable secure cookies to match Django settings")
	}
}

func TestLoadRejectsPoolInversion(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://pace:secret@db:5432/pace")
	t.Setenv("SECRET_KEY", "test-secret")
	t.Setenv("DB_MAX_OPEN_CONNS", "5")
	t.Setenv("DB_MAX_IDLE_CONNS", "6")
	if _, err := Load(); err == nil {
		t.Fatal("Load() should reject max idle connections above max open connections")
	}
}

func TestLoadRequiresSecretKey(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://pace:secret@db:5432/pace")
	t.Setenv("SECRET_KEY", "")
	if _, err := Load(); err == nil {
		t.Fatal("Load() should reject a missing SECRET_KEY")
	}
}

// TestTheListenAddressFollowsTheDjangoEntrypoint pins where the server binds.
//
// The Django entrypoint ran gunicorn with --bind 0.0.0.0:"${PORT:-8000}", and deployments rely on that: the all-in-one image puts the API on 3004 and its proxy sends /api/ there. Answering only to a name of this service's own would have come up on 8000 and been unreachable.
func TestTheListenAddressFollowsTheDjangoEntrypoint(t *testing.T) {
	for _, test := range []struct {
		name    string
		address string
		port    string
		want    string
	}{
		{name: "neither set", want: ":8000"},
		{name: "port alone", port: "3004", want: ":3004"},
		{name: "address alone", address: "127.0.0.1:9000", want: "127.0.0.1:9000"},
		{name: "address wins over port", address: "127.0.0.1:9000", port: "3004", want: "127.0.0.1:9000"},
		{name: "blank port is not a port", port: "  ", want: ":8000"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("PACE_API_ADDRESS", test.address)
			t.Setenv("PORT", test.port)
			if got := listenAddress(); got != test.want {
				t.Fatalf("listenAddress() = %q, want %q", got, test.want)
			}
		})
	}
}

// TestSecureCookiesDefaultToOn pins the direction of the default. The old rule answered no whenever nothing was configured, so a deployment that set no CORS origins quietly sent its session cookie over plain http.
func TestSecureCookiesDefaultToOn(t *testing.T) {
	for _, test := range []struct {
		name    string
		origins []string
		want    bool
	}{
		{"nothing configured", nil, true},
		{"all https", []string{"https://pace.example", "https://app.pace.example"}, true},
		{"one plain http", []string{"https://pace.example", "http://localhost:3000"}, false},
		{"only plain http", []string{"http://localhost:3000"}, false},
		{"upper case scheme", []string{"HTTP://localhost:3000"}, false},
	} {
		if got := secureCookieSetting(test.origins); got != test.want {
			t.Errorf("%s: secureCookieSetting(%v) = %v, want %v", test.name, test.origins, got, test.want)
		}
	}

	// The environment wins either way.
	t.Setenv("SECURE_COOKIES", "false")
	if secureCookieSetting([]string{"https://pace.example"}) {
		t.Error("SECURE_COOKIES=false did not turn it off")
	}
	t.Setenv("SECURE_COOKIES", "true")
	if !secureCookieSetting([]string{"http://localhost:3000"}) {
		t.Error("SECURE_COOKIES=true did not turn it on")
	}
	// Something unparseable falls back to the inference rather than guessing.
	t.Setenv("SECURE_COOKIES", "yes-please")
	if secureCookieSetting([]string{"http://localhost:3000"}) {
		t.Error("an unparseable SECURE_COOKIES should leave the inference in charge")
	}
}
