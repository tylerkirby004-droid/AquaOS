// Package api exposes operational HTTP endpoints. Domain handlers can later be
// mounted under /api/v1 without coupling the server to domain implementations.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/tylerkirby004-droid/aquaos/internal/config"
	"github.com/tylerkirby004-droid/aquaos/internal/health"
)

// API is the lifecycle boundary for an AquaOS HTTP adapter.
type API interface{ health.Component }

// Server exposes operational HTTP endpoints without domain business logic.
type Server struct {
	server           *http.Server
	health           *health.Manager
	logger           *slog.Logger
	mu               sync.RWMutex
	running          bool
	serveErr         error
	cancel           context.CancelFunc
	done             chan struct{}
	cancellationDone chan struct{}
	shutdownTimeout  time.Duration
}

// New constructs an HTTP server from external configuration and dependencies.
func New(cfg config.HTTP, manager *health.Manager, logger *slog.Logger) *Server {
	s := &Server{health: manager, logger: logger, shutdownTimeout: cfg.WriteTimeout}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", s.live)
	mux.HandleFunc("GET /health/ready", s.ready)
	mux.HandleFunc("GET /healthz", s.live)
	mux.HandleFunc("GET /readyz", s.ready)
	mux.HandleFunc("GET /api/v1/health", s.details)
	s.server = &http.Server{Addr: cfg.Address, Handler: mux, ReadTimeout: cfg.ReadTimeout, WriteTimeout: cfg.WriteTimeout, IdleTimeout: cfg.IdleTimeout, ReadHeaderTimeout: cfg.ReadTimeout}
	return s
}

// Name returns the lifecycle component name.
func (s *Server) Name() string { return "api" }

// Start listens and launches explicitly owned, context-cancellable HTTP goroutines.
func (s *Server) Start(ctx context.Context) error {
	listenConfig := net.ListenConfig{}
	listener, err := listenConfig.Listen(ctx, "tcp", s.server.Addr)
	if err != nil {
		return fmt.Errorf("listen HTTP: %w", err)
	}
	s.mu.Lock()
	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.done = make(chan struct{})
	s.cancellationDone = make(chan struct{})
	s.running = true
	s.serveErr = nil
	s.mu.Unlock()
	// The serving goroutine is owned by Server and joined by Stop. Cancellation
	// of the Start context triggers a bounded HTTP shutdown below.
	go func(done chan struct{}) {
		defer close(done)
		err := s.server.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.mu.Lock()
			s.serveErr = err
			s.running = false
			s.mu.Unlock()
			s.logger.Error("HTTP server stopped unexpectedly", "error", err)
		}
	}(s.done)
	// The cancellation watcher owns no work beyond bounded shutdown and is also
	// joined by Stop through cancellationDone.
	go func(done chan struct{}) {
		defer close(done)
		s.stopOnCancellation(runCtx)
	}(s.cancellationDone)
	s.logger.Info("HTTP server started", "address", listener.Addr().String())
	return nil
}

// Stop cancels, shuts down, and joins all server-owned goroutines.
func (s *Server) Stop(ctx context.Context) error {
	s.mu.RLock()
	cancel := s.cancel
	done := s.done
	cancellationDone := s.cancellationDone
	s.mu.RUnlock()
	if cancel != nil {
		cancel()
	}
	err := s.server.Shutdown(ctx)
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return errors.Join(err, ctx.Err())
		}
	}
	if cancellationDone != nil {
		select {
		case <-cancellationDone:
		case <-ctx.Done():
			return errors.Join(err, ctx.Err())
		}
	}
	s.mu.Lock()
	s.running = false
	s.mu.Unlock()
	return err
}

// stopOnCancellation makes the HTTP goroutine directly owned by the lifecycle
// context. It uses a separate timeout because the parent is already cancelled.
func (s *Server) stopOnCancellation(ctx context.Context) {
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), s.shutdownTimeout)
	defer cancel()
	if err := s.server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.Canceled) {
		s.logger.Error("HTTP context shutdown failed", "error", err)
	}
}

// Health reports whether the HTTP serving loop is running normally.
func (s *Server) Health() health.Status {
	s.mu.RLock()
	defer s.mu.RUnlock()
	state := health.StateUnhealthy
	if s.running && s.serveErr == nil {
		state = health.StateHealthy
	}
	status := health.NewStatus(s.Name(), state, "", time.Now().UTC())
	if s.serveErr != nil {
		status.Message = s.serveErr.Error()
	}
	return status
}

func (s *Server) live(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "alive"})
}
func (s *Server) ready(w http.ResponseWriter, _ *http.Request) {
	code := http.StatusOK
	report := s.health.Report()
	if !report.Ready {
		code = http.StatusServiceUnavailable
	}
	writeJSON(w, code, map[string]any{"ready": report.Ready, "state": report.State})
}
func (s *Server) details(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.health.Report())
}
func writeJSON(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}
