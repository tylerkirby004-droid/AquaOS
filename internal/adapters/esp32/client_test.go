package esp32

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPClientSnapshotContractAllowsAdditiveFields(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/aquaos/v1/snapshot" {
			t.Errorf("path = %s", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("missing bearer token")
		}
		_, _ = writer.Write([]byte(`{"schemaVersion":"1.0","nodeId":"11111111-1111-4111-8111-111111111111","firmware":"bench","bootId":"boot-a","sequence":1,"observedAt":"2026-08-06T12:00:00Z","probes":[],"futureField":true}`))
	}))
	defer server.Close()
	client, err := NewHTTPClient(server.Client())
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := client.Snapshot(context.Background(), server.URL, "test-token")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.SchemaVersion != "1.0" || snapshot.Sequence != 1 {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
}

func TestHTTPClientRejectsMalformedAndOversizedSnapshots(t *testing.T) {
	t.Parallel()
	for _, payload := range []string{`{"schemaVersion":`, string(make([]byte, maximumSnapshotBytes+1))} {
		payload := payload
		t.Run("invalid", func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { _, _ = writer.Write([]byte(payload)) }))
			defer server.Close()
			client, err := NewHTTPClient(server.Client())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.Snapshot(context.Background(), server.URL, ""); err == nil {
				t.Fatal("expected failure")
			}
		})
	}
}
