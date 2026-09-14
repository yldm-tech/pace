package config

import (
	"reflect"
	"testing"
	"time"
)

func TestLoadRequiresDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	if _, err := Load(); err == nil {
		t.Fatal("Load() should reject a missing DATABASE_URL")
	}
}

func TestLoadDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://pace:secret@db:5432/pace")
	t.Setenv("PACE_API_ADDRESS", "")
	t.Setenv("CORS_ORIGINS", "")
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
	t.Setenv("PACE_API_ADDRESS", "127.0.0.1:9000")
	t.Setenv("CORS_ORIGINS", "https://pace.example, https://admin.pace.example")
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
}

func TestLoadRejectsPoolInversion(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://pace:secret@db:5432/pace")
	t.Setenv("DB_MAX_OPEN_CONNS", "5")
	t.Setenv("DB_MAX_IDLE_CONNS", "6")
	if _, err := Load(); err == nil {
		t.Fatal("Load() should reject max idle connections above max open connections")
	}
}
