package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/lpernett/godotenv"
)

type Config struct {
	PORT                           string
	DATABASE_URL                   string
	REDIS_ADDR                     string
	REDIS_PASSWORD                 string
	REDIS_DB                       string
	RESEND_API_KEY                 string
	JWT_SECRET                     string
	ACCESS_TOKEN_TTL_MINUTES       string
	REFRESH_TOKEN_TTL_DAYS         string
	LLM_API_KEY                    string
	LLM_MODEL_NAME                 string
	LLM_EMBED_MODEL_NAME           string
	KAFKA_BROKERS                  string
	KAFKA_CLIENT_ID                string
	KAFKA_CONSUMER_GROUP           string
	KAFKA_SESSION_CONSUMER_GROUP   string
	TRUST_PROXY                    string
	RATE_LIMIT_REQUESTS            string
	RATE_LIMIT_WINDOW_SECONDS      string
	AUTH_RATE_LIMIT_REQUESTS       string
	AUTH_RATE_LIMIT_WINDOW_SECONDS string
}

var (
	dotEnvOnce sync.Once
	dotEnvErr  error
)

func loadDotEnv() error {
	dotEnvOnce.Do(func() {
		if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
			dotEnvErr = fmt.Errorf("load .env file: %w", err)
		}
	})

	return dotEnvErr
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok && value != "" {
		return value
	}

	return fallback
}

func Load() (Config, error) {
	if err := loadDotEnv(); err != nil {
		return Config{}, err
	}

	return Config{
		PORT:                           getEnv("PORT", "3000"),
		DATABASE_URL:                   getEnv("DATABASE_URL", ""),
		REDIS_ADDR:                     getEnv("REDIS_ADDR", "localhost:6379"),
		REDIS_PASSWORD:                 getEnv("REDIS_PASSWORD", ""),
		REDIS_DB:                       getEnv("REDIS_DB", "0"),
		RESEND_API_KEY:                 getEnv("RESEND_API_KEY", ""),
		JWT_SECRET:                     getEnv("JWT_SECRET", ""),
		ACCESS_TOKEN_TTL_MINUTES:       getEnv("ACCESS_TOKEN_TTL_MINUTES", "15"),
		REFRESH_TOKEN_TTL_DAYS:         getEnv("REFRESH_TOKEN_TTL_DAYS", "30"),
		LLM_API_KEY:                    getEnv("LLM_API_KEY", ""),
		LLM_MODEL_NAME:                 getEnv("LLM_MODEL_NAME", ""),
		LLM_EMBED_MODEL_NAME:           getEnv("LLM_EMBED_MODEL_NAME", ""),
		KAFKA_BROKERS:                  getEnv("KAFKA_BROKERS", "localhost:9092"),
		KAFKA_CLIENT_ID:                getEnv("KAFKA_CLIENT_ID", "janoluck"),
		KAFKA_CONSUMER_GROUP:           getEnv("KAFKA_CONSUMER_GROUP", "janoluck-email-worker"),
		KAFKA_SESSION_CONSUMER_GROUP:   getEnv("KAFKA_SESSION_CONSUMER_GROUP", "janoluck-session-worker"),
		TRUST_PROXY:                    getEnv("TRUST_PROXY", "false"),
		RATE_LIMIT_REQUESTS:            getEnv("RATE_LIMIT_REQUESTS", "240"),
		RATE_LIMIT_WINDOW_SECONDS:      getEnv("RATE_LIMIT_WINDOW_SECONDS", "60"),
		AUTH_RATE_LIMIT_REQUESTS:       getEnv("AUTH_RATE_LIMIT_REQUESTS", "10"),
		AUTH_RATE_LIMIT_WINDOW_SECONDS: getEnv("AUTH_RATE_LIMIT_WINDOW_SECONDS", "60"),
	}, nil
}

func (c Config) Require(names ...string) error {
	missing := make([]string, 0, len(names))

	for _, name := range names {
		value, ok := c.value(name)
		if !ok || strings.TrimSpace(value) == "" {
			missing = append(missing, name)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}

	return nil
}

func (c Config) value(name string) (string, bool) {
	switch name {
	case "PORT":
		return c.PORT, true
	case "DATABASE_URL":
		return c.DATABASE_URL, true
	case "REDIS_ADDR":
		return c.REDIS_ADDR, true
	case "REDIS_PASSWORD":
		return c.REDIS_PASSWORD, true
	case "REDIS_DB":
		return c.REDIS_DB, true
	case "RESEND_API_KEY":
		return c.RESEND_API_KEY, true
	case "JWT_SECRET":
		return c.JWT_SECRET, true
	case "ACCESS_TOKEN_TTL_MINUTES":
		return c.ACCESS_TOKEN_TTL_MINUTES, true
	case "REFRESH_TOKEN_TTL_DAYS":
		return c.REFRESH_TOKEN_TTL_DAYS, true
	case "LLM_API_KEY":
		return c.LLM_API_KEY, true
	case "LLM_MODEL_NAME":
		return c.LLM_MODEL_NAME, true
	case "LLM_EMBED_MODEL_NAME":
		return c.LLM_EMBED_MODEL_NAME, true
	case "KAFKA_BROKERS":
		return c.KAFKA_BROKERS, true
	case "KAFKA_CLIENT_ID":
		return c.KAFKA_CLIENT_ID, true
	case "KAFKA_CONSUMER_GROUP":
		return c.KAFKA_CONSUMER_GROUP, true
	case "KAFKA_SESSION_CONSUMER_GROUP":
		return c.KAFKA_SESSION_CONSUMER_GROUP, true
	case "TRUST_PROXY":
		return c.TRUST_PROXY, true
	case "RATE_LIMIT_REQUESTS":
		return c.RATE_LIMIT_REQUESTS, true
	case "RATE_LIMIT_WINDOW_SECONDS":
		return c.RATE_LIMIT_WINDOW_SECONDS, true
	case "AUTH_RATE_LIMIT_REQUESTS":
		return c.AUTH_RATE_LIMIT_REQUESTS, true
	case "AUTH_RATE_LIMIT_WINDOW_SECONDS":
		return c.AUTH_RATE_LIMIT_WINDOW_SECONDS, true
	default:
		return "", false
	}
}
