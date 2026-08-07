package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tylerkirby004-droid/aquaos/internal/alarms"
	"github.com/tylerkirby004-droid/aquaos/internal/config"
	"github.com/tylerkirby004-droid/aquaos/internal/domain"
	"github.com/tylerkirby004-droid/aquaos/internal/health"
	"github.com/tylerkirby004-droid/aquaos/internal/state"
)

const testToken = "this-is-a-test-token-not-a-secret"

type fakeState struct{ revision domain.Revision }

func (f fakeState) Get(context.Context, state.Key) (state.Value, error) {
	return state.Value{}, state.ErrNotFound
}
func (f fakeState) Snapshot(context.Context) (state.Snapshot, error) {
	return state.Snapshot{Revision: f.revision}, nil
}

type fakeAlarms struct{ acknowledgements int }

func (f *fakeAlarms) List(context.Context, alarms.Status) ([]alarms.Alarm, error) { return nil, nil }
func (f *fakeAlarms) Acknowledge(context.Context, domain.AlarmID) (alarms.Alarm, error) {
	f.acknowledgements++
	return alarms.Alarm{}, nil
}

type fakeConfiguration struct{ cfg config.Config }

func (f fakeConfiguration) Current() config.Config { return f.cfg }
func (f fakeConfiguration) Digest() string         { return "safe-digest" }
func (f fakeConfiguration) Plan(config.Config) (config.ReloadPlan, error) {
	return config.ReloadPlan{}, nil
}
func (f fakeConfiguration) Reload(context.Context) error { return nil }

func newSecuredTestServer(t *testing.T, dependencies Dependencies) *Server {
	t.Helper()
	authenticator, err := NewBearerAuthenticator(testToken, Principal{ID: "operator-test", Roles: []Role{RoleAdministrator}})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults().HTTP
	return New(cfg, health.NewManager(), slog.New(slog.NewTextHandler(io.Discard, nil)), WithDependencies(dependencies), WithSecurity(authenticator, nil))
}

func TestProtectedEndpointRejectsUnauthorizedRequest(t *testing.T) {
	server := newSecuredTestServer(t, Dependencies{})
	response := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/system", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", response.Code)
	}
	if contentType := response.Header().Get("Content-Type"); contentType != "application/problem+json" {
		t.Fatalf("content type = %q", contentType)
	}
}

func TestAlarmAcknowledgementRejectsStaleRevisionBeforeMutation(t *testing.T) {
	alarmService := &fakeAlarms{}
	server := newSecuredTestServer(t, Dependencies{State: fakeState{revision: 2}, Alarms: alarmService})
	body := bytes.NewBufferString(`{"reason":"operator reviewed","expectedRevision":1}`)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/alarms/11111111-1111-4111-8111-111111111111/ack", body)
	request.Header.Set("Authorization", "Bearer "+testToken)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	if alarmService.acknowledgements != 0 {
		t.Fatal("stale request reached alarm mutation")
	}
}

func TestDiagnosticsNeverExposeConfigurationSecrets(t *testing.T) {
	cfg := config.Defaults()
	cfg.MQTT.Password = "extremely-sensitive"
	cfg.HTTP.BearerTokenFile = "C:/secret/token"
	server := newSecuredTestServer(t, Dependencies{Configuration: fakeConfiguration{cfg: cfg}})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/diagnostics", nil)
	request.Header.Set("Authorization", "Bearer "+testToken)
	response := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	for _, secret := range []string{"extremely-sensitive", "C:/secret/token"} {
		if strings.Contains(response.Body.String(), secret) {
			t.Fatalf("diagnostics leaked %q", secret)
		}
	}
	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
}

func TestMutationBodyLimitIsEnforced(t *testing.T) {
	server := newSecuredTestServer(t, Dependencies{Configuration: fakeConfiguration{cfg: config.Defaults()}})
	server.maximumBodyBytes = 32
	request := httptest.NewRequest(http.MethodPost, "/api/v1/config/validate", strings.NewReader(strings.Repeat("x", 33)))
	request.Header.Set("Authorization", "Bearer "+testToken)
	response := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestCorrelationIDIsReturned(t *testing.T) {
	server := newSecuredTestServer(t, Dependencies{})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/system", nil)
	request.Header.Set("Authorization", "Bearer "+testToken)
	request.Header.Set(correlationHeader, "22222222-2222-4222-8222-222222222222")
	response := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(response, request)
	if got := response.Header().Get(correlationHeader); got != "22222222-2222-4222-8222-222222222222" {
		t.Fatalf("correlation ID = %q", got)
	}
}

func TestBearerAuthenticatorUsesConfiguredCredential(t *testing.T) {
	authenticator, err := NewBearerAuthenticator(testToken, Principal{ID: "test", Roles: []Role{RoleReader}})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Authorization", "Bearer wrong")
	if _, err = authenticator.Authenticate(request); err == nil {
		t.Fatal("wrong token authenticated")
	}
	request.Header.Set("Authorization", "Bearer "+testToken)
	principal, err := authenticator.Authenticate(request)
	if err != nil {
		t.Fatal(err)
	}
	if principal.ID != "test" {
		t.Fatalf("principal = %q", principal.ID)
	}
}

func TestRateLimiterRefillsWithoutGoroutine(t *testing.T) {
	limiter := newRateLimiter(1, 1)
	now := time.Unix(100, 0)
	if !limiter.allow("a", now) || limiter.allow("a", now) {
		t.Fatal("burst limit not enforced")
	}
	if !limiter.allow("a", now.Add(time.Second)) {
		t.Fatal("token did not refill")
	}
}
