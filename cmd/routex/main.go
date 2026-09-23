// Package main is the entry point for the application server.
package main

import (
	"context"
	"encoding/base64"
	"errors"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/fox-gonic/fox"

	"github.com/miclle/routex/internal/routex/config"
	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/handler"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/limits"
	"github.com/miclle/routex/pkg/secretstore"
)

var (
	// CommitID is the git commit hash, injected at build time via ldflags.
	CommitID = "dev"
	// BuildTime is the build timestamp, injected at build time via ldflags.
	BuildTime = ""
)

func main() {
	configPath := flag.String("c", "config.yaml", "path to config file")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, *configPath)
	stop()
	if err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

// run owns startup resources so every failure, including a listener failure,
// unwinds cleanup before main exits. Errors contain stage names, never DSNs,
// encryption material, upstream credentials, or raw configuration values.
func run(ctx context.Context, configPath string) (runErr error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return errors.New("load configuration failed")
	}
	store, err := credentialStore(cfg.EncryptionKey)
	if err != nil {
		return err
	}
	db, err := database.Open(ctx, cfg.Driver, cfg.DSN)
	if err != nil {
		return errors.New("open database failed")
	}
	pool, err := db.DB()
	if err != nil {
		return errors.New("access database pool failed")
	}
	defer func() {
		if err := pool.Close(); err != nil && runErr == nil {
			runErr = errors.New("close database failed")
		}
	}()
	if err := database.Migrate(ctx, db); err != nil {
		return errors.New("migrate database failed")
	}
	trustedProxies, err := limits.ParseTrustedProxies(cfg.TrustedProxies)
	if err != nil {
		return errors.New("invalid trusted proxy configuration")
	}
	svc, err := service.New(ctx, db, service.WithTrustedProxies(trustedProxies), service.WithCredentialStorage(store), service.WithUpstreamPolicy(cfg.AllowPrivateUpstreams), service.WithEgressPolicy(cfg.AllowPrivateEgresses), service.WithSMTPPolicy(cfg.AllowPrivateSMTP), service.WithStoragePolicy(cfg.AllowPrivateStorage))
	if err != nil {
		return errors.New("initialize service failed")
	}
	// Publishers and the recorder remain available while HTTP requests drain.
	// Their explicit stop methods run only after serveHTTP has shut the listener.
	lifecycle, cancel := context.WithCancel(context.WithoutCancel(ctx))
	defer cancel()
	defer func() {
		svc.StopRuntime()
		if err := svc.StopCallRecorder(); err != nil && runErr == nil {
			runErr = errors.New("close durable call buffer failed")
		}
	}()
	if ctx.Err() != nil {
		return nil
	}
	if err := svc.StartRuntime(lifecycle); err != nil {
		return errors.New("publish initial gateway runtime failed")
	}
	if err := svc.StartCallRecorder(lifecycle, cfg.EventQueuePath); err != nil {
		return errors.New("open durable call buffer failed")
	}
	if ctx.Err() != nil {
		return nil
	}
	stopStorageCleanup, err := svc.StartStorageCleanup(lifecycle)
	if err != nil {
		return errors.New("start storage cleanup failed")
	}
	defer stopStorageCleanup()
	engine := fox.Default()
	handler.New(svc).RegisterRoutes(engine)
	listener, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return errors.New("listen for HTTP requests failed")
	}
	log.Printf("server starting on %s (commit=%s, built=%s)", listener.Addr(), CommitID, BuildTime)
	return serveHTTP(ctx, listener, engine, 15*time.Second)
}

func credentialStore(encoded string) (*secretstore.Store, error) {
	if encoded == "" {
		return nil, nil
	}
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, errors.New("encryption_key must be a base64-encoded 32-byte key")
	}
	defer clear(key)
	store, err := secretstore.New(key)
	if err != nil {
		return nil, errors.New("encryption_key must be a base64-encoded 32-byte key")
	}
	return store, nil
}

func serveHTTP(ctx context.Context, listener net.Listener, handler http.Handler, grace time.Duration) error {
	requests, cancelRequests := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelRequests()
	server := &http.Server{
		Handler: handler, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: time.Minute,
		BaseContext: func(net.Listener) context.Context { return requests },
	}
	finished := make(chan error, 1)
	go func() { finished <- server.Serve(listener) }()
	select {
	case err := <-finished:
		_ = server.Close()
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return errors.New("HTTP server failed")
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), grace)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			// Cancel upstream work before closing active connections. Any final
			// fact not completed still has its durable interruption reservation.
			cancelRequests()
			_ = server.Close()
			<-finished
			return errors.New("HTTP shutdown exceeded its grace period")
		}
		<-finished
		return nil
	}
}
