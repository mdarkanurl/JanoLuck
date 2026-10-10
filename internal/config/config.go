package config

import (
	"os"

	"github.com/lpernett/godotenv"
)

type Config struct {
	PORT           string
	DATABASE_URL   string
	REDIS_ADDR     string
	REDIS_PASSWORD string
	REDIS_DB       string
}

func getEnv(key, fallback string) string {
	err := godotenv.Load()

	if err != nil {
		panic("Error while loading .env file")
	}

	value := os.Getenv(key)

	if value == "" {
		return fallback
	}

	return value
}

func Load() Config {
	return Config{
		PORT:           getEnv("PORT", "3000"),
		DATABASE_URL:   getEnv("DATABASE_URL", ""),
		REDIS_ADDR:     getEnv("REDIS_ADDR", "localhost:6379"),
		REDIS_PASSWORD: getEnv("REDIS_PASSWORD", ""),
		REDIS_DB:       getEnv("REDIS_DB", "0"),
	}
}
