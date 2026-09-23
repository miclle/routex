// Package service provides business logic and database operations.
package service

import (
	"context"
	"fmt"
	"net/http"
	"net/netip"
	"sync"
	"sync/atomic"

	"github.com/fox-gonic/fox/logger"
	"gorm.io/gorm"

	"github.com/miclle/routex/pkg/secretstore"
	"github.com/miclle/routex/pkg/upstream"
)

// Service holds the database connection and provides business logic methods.
type Service struct {
	db                   *gorm.DB
	limitMu              sync.RWMutex
	trustedProxies       []netip.Prefix
	runtime              *gatewayRuntime
	recorder             *callRecorder
	secrets              *secretstore.Store
	upstream             *http.Client
	allowPrivateUpstream bool
	allowPrivateEgress   bool
	egressMu             sync.RWMutex
	egressGeneration     atomic.Uint64
}

// Option configures bootstrap dependencies, never mutable business policy.
type Option func(*Service)

func WithCredentialStorage(store *secretstore.Store) Option {
	return func(s *Service) { s.secrets = store }
}

func WithUpstreamPolicy(allowPrivate bool) Option {
	return func(s *Service) {
		s.allowPrivateUpstream = allowPrivate
		s.upstream = upstream.NewClient(allowPrivate)
	}
}

// New creates a new Service instance with the given database handle.
func New(ctx context.Context, db *gorm.DB, options ...Option) (*Service, error) {
	l := logger.NewWithContext(ctx)

	if db == nil {
		return nil, fmt.Errorf("db handle is required")
	}

	l.Info("[Service] initialized")

	svc := &Service{db: db, upstream: upstream.NewClient(false)}
	for _, option := range options {
		option(svc)
	}
	return svc, nil
}

// DB returns the underlying GORM database connection.
func (s *Service) DB() *gorm.DB {
	return s.db
}
