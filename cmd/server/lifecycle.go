package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/config"
)

// newHTTPServer applies the configured timeouts to the academic HTTP server. Without
// them a client that stalls while sending headers, a body, or reading a response would
// hold a goroutine and a file descriptor indefinitely.
func newHTTPServer(cfg config.Config, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              "0.0.0.0:" + cfg.Port,
		Handler:           handler,
		ReadHeaderTimeout: time.Duration(cfg.ServerReadHeaderTimeout) * time.Second,
		ReadTimeout:       time.Duration(cfg.ServerReadTimeout) * time.Second,
		WriteTimeout:      time.Duration(cfg.ServerWriteTimeout) * time.Second,
		IdleTimeout:       time.Duration(cfg.ServerIdleTimeout) * time.Second,
	}
}

// serveUntilDone serves HTTP on an already bound listener until ctx is cancelled
// (SIGINT/SIGTERM) or the server fails. On cancellation it stops accepting connections
// and lets in-flight requests finish with Shutdown. When shutdownTimeout elapses first the
// remaining connections are closed, so the process always exits within the bound.
func serveUntilDone(ctx context.Context, server *http.Server, listener net.Listener, shutdownTimeout time.Duration) error {
	serveErr := make(chan error, 1)
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- fmt.Errorf("serve HTTP: %w", err)
			return
		}
		serveErr <- nil
	}()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
	}
	slog.Info("Shutdown signal received, draining HTTP", "timeout", shutdownTimeout.String())

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	var shutdownErr error
	if err := server.Shutdown(shutdownCtx); err != nil {
		shutdownErr = errors.Join(fmt.Errorf("HTTP shutdown did not finish in time, connections closed: %w", err), server.Close())
	}
	if err := errors.Join(shutdownErr, <-serveErr); err != nil {
		return err
	}
	slog.Info("Academic service stopped gracefully")
	return nil
}
