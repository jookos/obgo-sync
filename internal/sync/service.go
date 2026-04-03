package sync

import (
	"context"
	"errors"

	"github.com/jookos/obgo/internal/couchdb"
	"github.com/jookos/obgo/internal/crypto"
	"github.com/jookos/obgo/internal/watcher"
)

// ErrNotImplemented is returned by stub methods that are not yet implemented.
var ErrNotImplemented = errors.New("not implemented")

// Service orchestrates pull, push, and watch operations.
type Service struct {
	db       couchdb.Client
	crypto   *crypto.Service
	dataDir  string
	suppress *watcher.SuppressSet
}

// New creates a new sync Service.
func New(db couchdb.Client, cr *crypto.Service, dataDir string) *Service {
	return &Service{
		db:       db,
		crypto:   cr,
		dataDir:  dataDir,
		suppress: watcher.NewSuppressSet(),
	}
}

// Watch runs an initial Pull then starts concurrent watchers.
// Blocks until ctx is cancelled.
func (s *Service) Watch(ctx context.Context) error { return ErrNotImplemented }
