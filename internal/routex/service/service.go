// Package service provides business logic and database operations.
package service

import (
	"context"
	"fmt"
	"net/http"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fox-gonic/fox/logger"
	"gorm.io/gorm"

	"github.com/miclle/routex/pkg/secretstore"
	"github.com/miclle/routex/pkg/upstream"
	"github.com/miclle/routex/prices"
)

// Service holds the database connection and provides business logic methods.
type Service struct {
	db                        *gorm.DB
	credentialSources         credentialSourceHolders
	credentialCleanupMu       sync.Mutex
	credentialCreationHolders map[string]int
	credentialCleanupHolders  map[string]bool
	credentialValuesMu        sync.RWMutex
	credentialValues          map[string]string
	credentialAuthProofs      map[string]string
	limitMu                   sync.RWMutex
	trustedProxies            []netip.Prefix
	runtime                   *gatewayRuntime
	runtimeRefreshInterval    time.Duration
	recorder                  *callRecorder
	repositorySource          *prices.Snapshot
	secrets                   *secretstore.Store
	rootPolicy                atomic.Pointer[secretPolicyView]
	rootNow                   func() time.Time
	rootMutation              sync.Mutex
	rootReaders               atomic.Int64
	rootReadersClosed         atomic.Bool
	upstream                  *http.Client
	allowPrivateUpstream      bool
	allowPrivateSMTP          bool
	allowPrivateStorage       bool
	allowPrivateEgress        bool
	egressMu                  sync.RWMutex
	egressGeneration          atomic.Uint64
	attemptHealth             gatewayAttemptHealth
	attemptNow                func() time.Time
	afterGatewayAdmission     func()
	instanceStartMu           sync.Mutex
	instanceMu                sync.RWMutex
	instance                  *systemInstanceLease
	instanceNow               func() time.Time
	instanceResources         func(string) entitySystemInstanceResources
	instanceHeartbeat         time.Duration
	instanceLeaseDuration     time.Duration
	instanceCleanupAfter      time.Duration
}

// Option configures bootstrap dependencies, never mutable business policy.
type Option func(*Service)

func WithCredentialStorage(store *secretstore.Store) Option {
	return func(s *Service) { s.secrets = store }
}

// WithRuntimeRefreshInterval configures the bootstrap publisher cadence, not
// mutable runtime policy. The default remains one second.
func WithRuntimeRefreshInterval(interval time.Duration) Option {
	return func(s *Service) { s.runtimeRefreshInterval = interval }
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

	svc := &Service{
		db: db, upstream: upstream.NewClient(false), attemptNow: time.Now,
		runtimeRefreshInterval: runtimeRefreshInterval,
		rootNow:                time.Now, instanceNow: time.Now, instanceResources: collectSystemInstanceResources,
		instanceHeartbeat: 10 * time.Second, instanceLeaseDuration: 35 * time.Second,
		instanceCleanupAfter: 5 * time.Minute,
	}
	for _, option := range options {
		option(svc)
	}
	if svc.runtimeRefreshInterval < time.Millisecond || svc.runtimeRefreshInterval > 24*time.Hour {
		return nil, fmt.Errorf("runtime refresh interval must be between one millisecond and 24 hours")
	}
	return svc, nil
}

// DB returns the underlying GORM database connection.
func (s *Service) DB() *gorm.DB {
	return s.db
}
