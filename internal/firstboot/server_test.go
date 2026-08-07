package firstboot

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeRunner struct{ called bool }

func (f *fakeRunner) Install(context.Context, InstallRequest) error { f.called = true; return nil }

func TestInstallRequiresAuthenticationAndAcknowledgements(t *testing.T) {
	runner := &fakeRunner{}
	server, err := NewServer(context.Background(), "0123456789abcdef0123456789abcdef", "192.168.1.50", runner)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"siteId":"home-reef","address":"192.168.1.50","timezone":"UTC","dedicatedAppliance":true,"independentSafeguards":true}`)
	request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/install", bytes.NewReader(body))
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", response.Code)
	}
	request = httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/install", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer 0123456789abcdef0123456789abcdef")
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || !runner.called {
		t.Fatalf("status = %d, called = %v", response.Code, runner.called)
	}
}

func TestValidateRejectsPublicAddress(t *testing.T) {
	err := Validate(InstallRequest{SiteID: "home-reef", Address: "8.8.8.8", Timezone: "UTC", DedicatedAppliance: true, IndependentSafeguards: true})
	if err == nil {
		t.Fatal("public address accepted")
	}
}
