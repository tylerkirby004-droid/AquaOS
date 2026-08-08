package admin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tylerkirby004-droid/aquaos/internal/config"
	"github.com/tylerkirby004-droid/aquaos/internal/discovery"
	"github.com/tylerkirby004-droid/aquaos/internal/operations"
)

type fakeOperations struct {
	repairs   int
	statusErr error
}

type fakeDiscovery struct{}
type fakeRuntime struct{}

func (fakeRuntime) Report(context.Context) (any, error) {
	return map[string]any{"health": map[string]any{"state": "degraded"}}, nil
}

func (fakeDiscovery) Probe(context.Context, []discovery.Candidate) ([]discovery.Result, error) {
	return []discovery.Result{{Kind: discovery.KindShelly, BaseURL: "http://plug.local", Reachable: true}}, nil
}

func (f *fakeOperations) GetStatus(context.Context, operations.Actor) (operations.Status, error) {
	return operations.Status{Installed: true, Version: "test"}, f.statusErr
}
func (*fakeOperations) GetConfiguration(context.Context, operations.Actor) (config.Config, error) {
	return config.Defaults(), nil
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
	server, err := New(Config{Address: "127.0.0.1:0", Token: "test-token-with-at-least-32-characters", MaximumRequestBytes: 64 * 1024, ShutdownTimeout: time.Second, AuthenticationRate: 100, AuthenticationBurst: 100, MutationRate: 100, MutationBurst: 100}, service, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return server
}
func TestAdminAPIRequiresAuthentication(t *testing.T) {
	server := newTestServer(t, &fakeOperations{})
	response := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(response, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/status", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", response.Code)
	}
}
func TestTrustedIngressUsesProxyAuthentication(t *testing.T) {
	server, err := New(Config{Address: "127.0.0.1:0", TrustedIngress: true, TrustedIngressCIDR: "192.0.2.1/32", MaximumRequestBytes: 64 * 1024, ShutdownTimeout: time.Second, AuthenticationRate: 100, AuthenticationBurst: 100, MutationRate: 100, MutationBurst: 100}, &fakeOperations{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(response, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/status", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("trusted ingress status = %d", response.Code)
	}
	if response.Header().Get("X-Frame-Options") != "" || !strings.Contains(response.Header().Get("Content-Security-Policy"), "frame-ancestors 'self'") {
		t.Fatal("trusted ingress response cannot be embedded safely")
	}
}
func TestTrustedIngressRejectsOtherInternalClients(t *testing.T) {
	server, err := New(Config{Address: "127.0.0.1:0", TrustedIngress: true, TrustedIngressCIDR: "192.0.2.2/32", MaximumRequestBytes: 64 * 1024, ShutdownTimeout: time.Second, AuthenticationRate: 100, AuthenticationBurst: 100, MutationRate: 100, MutationBurst: 100}, &fakeOperations{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(response, httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/repair", strings.NewReader(`{"dryRun":true}`)))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("untrusted internal client status = %d", response.Code)
	}
}
func TestAdminMutationCallsAuthorizedApplicationService(t *testing.T) {
	service := &fakeOperations{}
	server := newTestServer(t, service)
	request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/repair", strings.NewReader(`{"dryRun":true}`))
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
	server.server.Handler.ServeHTTP(response, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Let’s set up your aquarium") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestAdminRootServesIngressUI(t *testing.T) {
	server := newTestServer(t, &fakeOperations{})
	response := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(response, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Let’s set up your aquarium") {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestEmbeddedAdminUIAcceptsAndClearsFirstBootHandoff(t *testing.T) {
	payload, err := assets.ReadFile("web/app.js")
	if err != nil {
		t.Fatal(err)
	}
	contents := string(payload)
	for _, required := range []string{"access_token", "history.replaceState", "resumeSession()", "applicationBase", "api/session", "renderServiceLinks()", "homeAssistantSession", "function uuid()", "crypto.getRandomValues"} {
		if !strings.Contains(contents, required) {
			t.Fatalf("Admin UI does not contain secure first-boot handoff %q", required)
		}
	}
	style, err := assets.ReadFile("web/style.css")
	if err != nil {
		t.Fatal(err)
	}
	for _, color := range []string{"#1e1e1e", "#252526", "#cccccc"} {
		if !strings.Contains(string(style), color) {
			t.Fatalf("Admin UI dark theme is missing neutral color %q", color)
		}
	}
}

func TestAdminPairingCreatesPersistentProtectedSession(t *testing.T) {
	server := newTestServer(t, &fakeOperations{})
	pair := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/session", nil)
	pair.Header.Set("Authorization", "Bearer "+server.cfg.Token)
	pairResponse := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(pairResponse, pair)
	if pairResponse.Code != http.StatusNoContent {
		t.Fatalf("pair status = %d", pairResponse.Code)
	}
	cookies := pairResponse.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode || cookies[0].MaxAge <= 0 {
		t.Fatalf("session cookie is not persistently protected: %+v", cookies)
	}
	if cookies[0].Value == server.cfg.Token {
		t.Fatal("session cookie exposes the administrator token")
	}
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/status", nil)
	request.AddCookie(cookies[0])
	response := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("cookie-authenticated status = %d", response.Code)
	}
}

func TestAdminProductionPairingCookieIsSecure(t *testing.T) {
	server := newTestServer(t, &fakeOperations{})
	server.secureCookie = true
	request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/session", nil)
	request.Header.Set("Authorization", "Bearer "+server.cfg.Token)
	response := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(response, request)
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure {
		t.Fatalf("production session cookie is not Secure: %+v", cookies)
	}
}

func TestAdminReturnsRedactedConfigurationThroughApplicationService(t *testing.T) {
	server := newTestServer(t, &fakeOperations{})
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/config", nil)
	request.Header.Set("Authorization", "Bearer test-token-with-at-least-32-characters")
	response := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"schemaVersion":1`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestAdminValidatesStructuredConfigurationThroughApplicationService(t *testing.T) {
	server := newTestServer(t, &fakeOperations{})
	payload, err := json.Marshal(editableConfiguration{Configuration: config.Defaults()})
	if err != nil {
		t.Fatal(err)
	}
	var decoded editableConfiguration
	if err = json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/config/editable/validate", strings.NewReader(string(payload)))
	request.Header.Set("Authorization", "Bearer test-token-with-at-least-32-characters")
	response := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestAdminDiscoveryUsesBoundedReadOnlyService(t *testing.T) {
	base := newTestServer(t, &fakeOperations{})
	server, err := New(base.cfg, &fakeOperations{}, slog.New(slog.NewTextHandler(io.Discard, nil)), WithDiscovery(fakeDiscovery{}))
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/discovery/probe", strings.NewReader(`{"candidates":[{"kind":"shelly","baseUrl":"http://plug.local"}]}`))
	request.Header.Set("Authorization", "Bearer test-token-with-at-least-32-characters")
	response := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"reachable":true`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestAdminRuntimeStatusUsesReadOnlyCoreBoundary(t *testing.T) {
	base := newTestServer(t, &fakeOperations{})
	server, err := New(base.cfg, &fakeOperations{}, slog.New(slog.NewTextHandler(io.Discard, nil)), WithRuntimeHealth(fakeRuntime{}))
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/runtime", nil)
	request.Header.Set("Authorization", "Bearer test-token-with-at-least-32-characters")
	response := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"degraded"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestAdminRejectsIncompleteTLSIdentity(t *testing.T) {
	base := newTestServer(t, &fakeOperations{})
	base.cfg.TLSCertificateFile = "certificate.pem"
	if _, err := New(base.cfg, &fakeOperations{}, slog.Default()); err == nil {
		t.Fatal("incomplete TLS identity accepted")
	}
}
func TestRestorePayloadIsBounded(t *testing.T) {
	server := newTestServer(t, &fakeOperations{})
	request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/restore", strings.NewReader(strings.Repeat("x", 64*1024+1)))
	request.Header.Set("Authorization", "Bearer test-token-with-at-least-32-characters")
	response := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestAdminSecurityHeadersAndOriginPolicy(t *testing.T) {
	server := newTestServer(t, &fakeOperations{})
	request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/repair", strings.NewReader(`{"dryRun":true}`))
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

func TestAdminAuthenticationLimiterIsBoundedAndEnforced(t *testing.T) {
	server := newTestServer(t, &fakeOperations{})
	server.authentication = newRequestLimiter(1, 1, 2)
	for attempt := 0; attempt < 2; attempt++ {
		response := httptest.NewRecorder()
		server.server.Handler.ServeHTTP(response, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/status", nil))
		if attempt == 1 && response.Code != http.StatusTooManyRequests {
			t.Fatalf("status = %d", response.Code)
		}
	}
	server.authentication.allow("second", time.Now())
	server.authentication.allow("third", time.Now())
	if len(server.authentication.clients) != 2 {
		t.Fatalf("clients = %d", len(server.authentication.clients))
	}
}

func TestAdminOperationErrorIsNotExposed(t *testing.T) {
	server := newTestServer(t, &fakeOperations{statusErr: errors.New("secret=/etc/aquaos/token")})
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/status", nil)
	request.Header.Set("Authorization", "Bearer test-token-with-at-least-32-characters")
	response := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(response, request)
	if strings.Contains(response.Body.String(), "secret") || strings.Contains(response.Body.String(), "/etc/aquaos/token") {
		t.Fatalf("internal error leaked: %s", response.Body.String())
	}
}
