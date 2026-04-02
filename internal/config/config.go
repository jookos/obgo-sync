package config

import (
	"errors"
	"os"
)

// Config holds the runtime configuration for obgo-live.
type Config struct {
	CouchDBURL   string
	E2EEPassword string
	DataPath     string
}

// Load reads COUCHDB_URL, E2EE_PASSWORD, OBGO_DATA from environment.
// Returns error if COUCHDB_URL or OBGO_DATA are empty.
func Load() (*Config, error) {
	cfg := &Config{
		CouchDBURL:   os.Getenv("COUCHDB_URL"),
		E2EEPassword: os.Getenv("E2EE_PASSWORD"),
		DataPath:     os.Getenv("OBGO_DATA"),
	}

	if cfg.CouchDBURL == "" {
		return nil, errors.New("COUCHDB_URL is required")
	}
	if cfg.DataPath == "" {
		return nil, errors.New("OBGO_DATA is required")
	}

	return cfg, nil
}
