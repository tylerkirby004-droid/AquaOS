package admin

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tylerkirby004-droid/aquaos/internal/operations"
)

type fakeOperations struct{ repairs int }

func (*fakeOperations) GetStatus(context.Context, operations.Actor) (operations.Status, error) {
	return operations.Status{Installed: true, Version: "test"}, nil
}
func (*fakeOperations) Verify(context.Context, operations.Actor) (operations.Diagnostics, error) {
	return operations.Diagnostics{}, nil
}
func (f *fakeOperations) Repair(_ context.Context, actor operations.Actor, _ bool) (operations.Result, error) {
	if !actor.Administrator {
		return operations.Result{}, context.Canceled
	}
	f.repairs++
	return operations.Result{Operation: "repair"}, nil
}
func (*fakeOperations) Install(context.Context, operations.InstallRequest) (operations.Result, error) {
	return operations.Result{}, nil
}
func (*fakeOperations) Upgrade(context.Context, operations.UpgradeRequest) (operations.Result, error) {
	return operations.Result{}, nil
}
func (*fakeOperations) Rollback(context.Context, operations.Actor, bool) (operations.Result, error) {
	return operations.Result{}, nil
}
func (*fakeOperations) Backup(context.Context, operations.Actor) ([]byte, error) {
	return []byte("backup"), nil
}
func (*fakeOperations) Restore(context.Context, operations.Actor, []byte, bool) (operations.Result, error) {
	return operations.Result{}, nil
}
func (*fakeOperations) Uninstall(context.Context, operations.Actor, bool, bool) (operations.Result, error) {
	return operations.Result{}, nil
}
func (*fakeOperations) ValidateConfiguration(context.Context, operations.Actor, []byte) (operations.ConfigurationResult, error) {
	return operations.ConfigurationResult{Valid: true}, nil
}
func (*fakeOperations) ApplyConfiguration(context.Context, operations.Actor, []byte, bool) (operations.ConfigurationResult, error) {
	return operations.ConfigurationResult{Valid: true, Changed: true}, nil
}
func newTestServer(t *testing.T, service Operations) *Server {
	t.Helper()
	server, err := New(Config{Address: "127.0.0.1:0", Token: "test-token-with-at-least-32-characters", MaximumRequestBytes: 1024, ShutdownTimeout: time.Second}, service, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return server
}
func TestAdminAPIRequiresAuthentication(t *testing.T) {
	server := newTestServer(t, &fakeOperations{})
	response := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/status", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", response.Code)
	}
}
func TestAdminMutationCallsAuthorizedApplicationService(t *testing.T) {
	service := &fakeOperations{}
	server := newTestServer(t, service)
	request := httptest.NewRequest(http.MethodPost, "/api/repair", strings.NewReader(`{"dryRun":true}`))
	request.Header.Set("Authorization", "Bearer test-token-with-at-least-32-characters")
	response := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || service.repairs != 1 {
		t.Fatalf("status=%d repairs=%d body=%s", response.Code, service.repairs, response.Body.String())
	}
}
func TestEmbeddedAdminUIIsAvailableWithoutNodeBuild(t *testing.T) {
	server := newTestServer(t, &fakeOperations{})
	response := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/admin/", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Recovery-safe administration") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
func TestRestorePayloadIsBounded(t *testing.T) {
	server := newTestServer(t, &fakeOperations{})
	request := httptest.NewRequest(http.MethodPost, "/api/restore", strings.NewReader(strings.Repeat("x", 1025)))
	request.Header.Set("Authorization", "Bearer test-token-with-at-least-32-characters")
	response := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestAdminSecurityHeadersAndOriginPolicy(t *testing.T) {
	server := newTestServer(t, &fakeOperations{})
	request := httptest.NewRequest(http.MethodPost, "/api/repair", strings.NewReader(`{"dryRun":true}`))
	request.Host = "127.0.0.1:8090"
	request.Header.Set("Authorization", "Bearer test-token-with-at-least-32-characters")
	request.Header.Set("Origin", "https://attacker.example")
	response := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d", response.Code)
	}
	if response.Header().Get("X-Frame-Options") != "DENY" || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("required browser security headers were not set")
	}
}
