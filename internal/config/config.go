package config

import (
	"os"

	"github.com/lpernett/godotenv"
)

type Config struct {
	PORT string
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
		PORT: getEnv("PORT", "3000"),
	}
}
