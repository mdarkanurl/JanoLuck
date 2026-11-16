package config

import (
	"strings"
	"testing"
)

func TestLoadWithoutDotEnv(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example/db")
	t.Setenv("JWT_SECRET", "unit-test-secret")
	t.Setenv("KAFKA_BROKERS", "broker:9092")
	t.Setenv("LLM_API_KEY", "llm-key")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.DATABASE_URL != "postgres://example/db" {
		t.Errorf("DATABASE_URL = %q", cfg.DATABASE_URL)
	}
	if cfg.JWT_SECRET != "unit-test-secret" {
		t.Errorf("JWT_SECRET = %q", cfg.JWT_SECRET)
	}
	// Defaults still apply when a value is not set.
	if cfg.PORT != "3000" {
		t.Errorf("PORT = %q, want the default 3000", cfg.PORT)
	}
	if cfg.KAFKA_CLIENT_ID != "janoluck" {
		t.Errorf("KAFKA_CLIENT_ID = %q, want the default", cfg.KAFKA_CLIENT_ID)
	}

	// There is no insecure JWT fallback anymore.
	t.Setenv("JWT_SECRET", "")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.JWT_SECRET != "" {
		t.Errorf("JWT_SECRET = %q, want empty so Require rejects it", cfg.JWT_SECRET)
	}
}

func TestRequire(t *testing.T) {
	cfg := Config{
		DATABASE_URL:  "postgres://example/db",
		JWT_SECRET:    "  ",
		KAFKA_BROKERS: "",
		LLM_API_KEY:   "llm-key",
	}

	err := cfg.Require("DATABASE_URL", "JWT_SECRET", "KAFKA_BROKERS", "LLM_API_KEY")
	if err == nil {
		t.Fatal("expected missing settings to be reported")
	}
	message := err.Error()
	for _, name := range []string{"JWT_SECRET", "KAFKA_BROKERS"} {
		if !strings.Contains(message, name) {
			t.Errorf("error %q does not mention %s", message, name)
		}
	}
	for _, name := range []string{"DATABASE_URL", "LLM_API_KEY"} {
		if strings.Contains(message, name) {
			t.Errorf("error %q should not mention the configured %s", message, name)
		}
	}

	if err := cfg.Require("DATABASE_URL", "LLM_API_KEY"); err != nil {
		t.Errorf("Require() error = %v, want nil", err)
	}
}
