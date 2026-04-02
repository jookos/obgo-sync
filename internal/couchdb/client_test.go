package couchdb

import (
	"context"
	"errors"
	"testing"
)

func TestNew_ParsesURL(t *testing.T) {
	rawURL := "http://admin:password@localhost:5984/my-vault"
	c, err := New(rawURL)
	if err != nil {
		t.Fatalf("New() unexpected error: %v", err)
	}
	if c.dbName != "my-vault" {
		t.Errorf("expected dbName %q, got %q", "my-vault", c.dbName)
	}
	if c.username != "admin" {
		t.Errorf("expected username %q, got %q", "admin", c.username)
	}
	if c.password != "password" {
		t.Errorf("expected password %q, got %q", "password", c.password)
	}
	if c.baseURL.Host != "localhost:5984" {
		t.Errorf("expected host %q, got %q", "localhost:5984", c.baseURL.Host)
	}
}

func TestNew_MissingDBName(t *testing.T) {
	_, err := New("http://admin:password@localhost:5984/")
	if err == nil {
		t.Fatal("expected error for missing db name, got nil")
	}
}

func TestNew_InvalidURL(t *testing.T) {
	_, err := New("://invalid")
	if err == nil {
		t.Fatal("expected error for invalid URL, got nil")
	}
}

func TestHTTPClient_UnimplementedMethod(t *testing.T) {
	c, err := New("http://admin:password@localhost:5984/vault")
	if err != nil {
		t.Fatalf("New() unexpected error: %v", err)
	}
	ctx := context.Background()
	_, err = c.AllMetaDocs(ctx)
	if !errors.Is(err, ErrNotImplemented) {
		t.Errorf("expected ErrNotImplemented, got %v", err)
	}
}
