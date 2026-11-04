package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// credentials is the local CLI session. It is stored in the user's config
// directory with 0600 permissions.
type credentials struct {
	Email            string    `json:"email,omitempty"`
	AccessToken      string    `json:"accessToken,omitempty"`
	RefreshToken     string    `json:"refreshToken,omitempty"`
	AccessExpiresAt  time.Time `json:"accessExpiresAt,omitempty"`
	RefreshExpiresAt time.Time `json:"refreshExpiresAt,omitempty"`
	LastSessionID    string    `json:"lastSessionId,omitempty"`
}

func statePath() (string, error) {
	if path := os.Getenv("JANOLUCK_CONFIG"); path != "" {
		return path, nil
	}

	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("find config directory: %w", err)
	}

	return filepath.Join(dir, "janoluck", "auth.json"), nil
}

func loadCredentials(path string) (credentials, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return credentials{}, nil
		}
		return credentials{}, fmt.Errorf("read credentials: %w", err)
	}

	if len(data) == 0 {
		return credentials{}, nil
	}

	var creds credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return credentials{}, fmt.Errorf("credentials file %s is corrupt: %w", path, err)
	}

	return creds, nil
}

func saveCredentials(path string, creds credentials) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("encode credentials: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(dir, "auth-*.json")
	if err != nil {
		return fmt.Errorf("create credentials file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("secure credentials file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write credentials: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write credentials: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("save credentials: %w", err)
	}

	return nil
}

func removeCredentials(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove credentials: %w", err)
	}
	return nil
}
