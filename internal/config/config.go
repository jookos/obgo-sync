package config

import (
	"errors"
	"os"
	"strings"
)

// Config holds the runtime configuration for obgo-live.
type Config struct {
	CouchDBURL        string
	E2EEPassword      string
	DataPath          string
	PathObfuscation   string // raw value of OBGO_PATH_OBFUSCATION
}

// PathObfuscationMode normalises OBGO_PATH_OBFUSCATION to one of "auto", "on", "off".
func (c *Config) PathObfuscationMode() string {
	switch strings.ToLower(c.PathObfuscation) {
	case "true", "yes", "1", "on":
		return "on"
	case "false", "no", "0", "off":
		return "off"
	default:
		return "auto"
	}
}

// Load reads COUCHDB_URL, E2EE_PASSWORD, OBGO_DATA, OBGO_PATH_OBFUSCATION from environment.
// Returns error if COUCHDB_URL or OBGO_DATA are empty.
func Load() (*Config, error) {
	cfg := &Config{
		CouchDBURL:      os.Getenv("COUCHDB_URL"),
		E2EEPassword:    os.Getenv("E2EE_PASSWORD"),
		DataPath:        os.Getenv("OBGO_DATA"),
		PathObfuscation: os.Getenv("OBGO_PATH_OBFUSCATION"),
	}

	if cfg.CouchDBURL == "" {
		return nil, errors.New("COUCHDB_URL is required")
	}
	if cfg.DataPath == "" {
		return nil, errors.New("OBGO_DATA is required")
	}

	return cfg, nil
}
