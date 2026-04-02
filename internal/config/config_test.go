package config

import (
	"testing"
)

func TestLoad_MissingCouchDBURL(t *testing.T) {
	t.Setenv("COUCHDB_URL", "")
	t.Setenv("OBGO_DATA", "/tmp/vault")
	t.Setenv("E2EE_PASSWORD", "")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error when COUCHDB_URL is empty, got nil")
	}
}

func TestLoad_MissingOBGOData(t *testing.T) {
	t.Setenv("COUCHDB_URL", "http://admin:password@localhost:5984/vault")
	t.Setenv("OBGO_DATA", "")
	t.Setenv("E2EE_PASSWORD", "")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error when OBGO_DATA is empty, got nil")
	}
}

func TestLoad_AllVarsSet(t *testing.T) {
	t.Setenv("COUCHDB_URL", "http://admin:password@localhost:5984/vault")
	t.Setenv("OBGO_DATA", "/tmp/vault")
	t.Setenv("E2EE_PASSWORD", "supersecret")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.CouchDBURL != "http://admin:password@localhost:5984/vault" {
		t.Errorf("unexpected CouchDBURL: %q", cfg.CouchDBURL)
	}
	if cfg.DataPath != "/tmp/vault" {
		t.Errorf("unexpected DataPath: %q", cfg.DataPath)
	}
	if cfg.E2EEPassword != "supersecret" {
		t.Errorf("unexpected E2EEPassword: %q", cfg.E2EEPassword)
	}
}

func TestLoad_E2EEPasswordOptional(t *testing.T) {
	t.Setenv("COUCHDB_URL", "http://admin:password@localhost:5984/vault")
	t.Setenv("OBGO_DATA", "/tmp/vault")
	t.Setenv("E2EE_PASSWORD", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.E2EEPassword != "" {
		t.Errorf("expected empty E2EEPassword, got %q", cfg.E2EEPassword)
	}
}
