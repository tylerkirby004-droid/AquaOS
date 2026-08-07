package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tylerkirby004-droid/aquaos/internal/config"
	"github.com/tylerkirby004-droid/aquaos/internal/health"
)

func TestLivenessHandler(t *testing.T) {
	server := &Server{health: health.NewManager(), logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	response := httptest.NewRecorder()
	server.live(response, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/healthz", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestCanonicalHealthRoutesAreRegistered(t *testing.T) {
	cfg := config.HTTP{Address: ":0", ReadTimeout: time.Second, WriteTimeout: time.Second, IdleTimeout: time.Second}
	server := New(cfg, health.NewManager(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	for _, path := range []string{"/health/live", "/health/ready"} {
		response := httptest.NewRecorder()
		server.server.Handler.ServeHTTP(response, httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil))
		if response.Code == http.StatusNotFound {
			t.Fatalf("route %s was not registered", path)
		}
	}
}

func TestReadinessReflectsAggregateHealth(t *testing.T) {
	manager := health.NewManager()
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	server := &Server{health: manager, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	response := httptest.NewRecorder()
	server.ready(response, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/health/ready", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("healthy status = %d", response.Code)
	}
	if err := manager.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	server.ready(response, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/health/ready", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("unhealthy status = %d", response.Code)
	}
}
