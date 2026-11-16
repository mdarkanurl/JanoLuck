package server

import (
	"strings"
	"testing"
)

func TestNewFailsFastWithoutRequiredConfig(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("JWT_SECRET", "")
	t.Setenv("LLM_API_KEY", "")
	t.Setenv("KAFKA_BROKERS", " ")

	_, err := New()
	if err == nil {
		t.Fatal("expected New() to reject missing required configuration")
	}

	for _, name := range []string{"DATABASE_URL", "JWT_SECRET", "LLM_API_KEY", "KAFKA_BROKERS"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error %q does not mention %s", err, name)
		}
	}
}
