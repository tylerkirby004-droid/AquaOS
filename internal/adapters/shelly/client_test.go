package shelly

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPClientSwitchContractsAndSchemaVariation(t *testing.T) {
	t.Parallel()
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		methods = append(methods, request.URL.Path)
		writer.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(request.URL.Path, "GetStatus") {
			_, _ = writer.Write([]byte(`{"id":0,"source":"HTTP_in","output":true,"apower":41.2,"voltage":120.1,"current":0.34,"future_field":{"ignored":true}}`))
			return
		}
		if strings.HasSuffix(request.URL.Path, "GetConfig") {
			_, _ = writer.Write([]byte(`{"id":0,"initial_state":"off","future_field":true}`))
			return
		}
		_, _ = writer.Write([]byte(`{"was_on":false,"future_field":1}`))
	}))
	defer server.Close()

	client, err := NewHTTPClient(server.Client())
	if err != nil {
		t.Fatal(err)
	}
	status, err := client.GetSwitchStatus(context.Background(), server.URL, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Output || status.APower != 41.2 || status.Source != "HTTP_in" {
		t.Fatalf("unexpected status: %+v", status)
	}
	if _, err := client.SetSwitch(context.Background(), server.URL, 0, true); err != nil {
		t.Fatal(err)
	}
	config, err := client.GetSwitchConfig(context.Background(), server.URL, 0)
	if err != nil || config.InitialState != "off" {
		t.Fatalf("unexpected config: %+v %v", config, err)
	}
	if len(methods) != 3 || methods[0] != "/rpc/Switch.GetStatus" || methods[1] != "/rpc/Switch.Set" || methods[2] != "/rpc/Switch.GetConfig" {
		t.Fatalf("unexpected RPC paths: %v", methods)
	}
}

func TestHTTPClientRejectsProtocolFailures(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		handler http.Handler
	}{
		{"http error", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "no", http.StatusServiceUnavailable) })},
		{"malformed", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"id":`)) })},
		{"oversized", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(strings.Repeat("x", maximumResponseBytes+1)))
		})},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(test.handler)
			defer server.Close()
			client, err := NewHTTPClient(server.Client())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.GetSwitchStatus(context.Background(), server.URL, 0); err == nil {
				t.Fatal("expected protocol failure")
			}
		})
	}
}

func TestRPCURLRejectsCredentialsAndNonHTTP(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"https://device.local", "http://user:secret@device.local", "http://device.local?x=1"} {
		if _, err := rpcURL(value, "Switch.GetStatus"); err == nil {
			t.Fatalf("expected %q to be rejected", value)
		}
	}
}
