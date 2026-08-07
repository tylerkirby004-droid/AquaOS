package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCoreHealthClientMergesHealthAndCanonicalState(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer secret" {
			t.Fatal("Core bearer token missing")
		}
		if request.URL.Path == "/api/v1/health" {
			_ = json.NewEncoder(w).Encode(map[string]any{"state": "healthy"})
			return
		}
		if request.URL.Path == "/api/v1/state" {
			_ = json.NewEncoder(w).Encode(map[string]any{"revision": 1})
			return
		}
		http.NotFound(w, request)
	}))
	defer server.Close()
	client := &coreHealthClient{client: server.Client(), baseURL: server.URL, token: "secret"}
	value, err := client.Report(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	result, ok := value.(map[string]any)
	if !ok || result["health"] == nil || result["canonicalState"] == nil {
		t.Fatalf("report = %#v", value)
	}
}
