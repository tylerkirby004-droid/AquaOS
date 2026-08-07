package api

import (
	"encoding/json"
	"net/http"
)

// FieldError identifies one invalid request field without echoing its value.
type FieldError struct {
	Field string `json:"field"`
	Code  string `json:"code"`
}

// Problem is the stable RFC 9457-compatible AquaOS error envelope.
type Problem struct {
	Type          string       `json:"type"`
	Title         string       `json:"title"`
	Status        int          `json:"status"`
	Code          string       `json:"code"`
	Detail        string       `json:"detail,omitempty"`
	Instance      string       `json:"instance,omitempty"`
	CorrelationID string       `json:"correlationId"`
	Fields        []FieldError `json:"fieldErrors,omitempty"`
}

func writeProblem(w http.ResponseWriter, request *http.Request, status int, code, title, detail string, fields ...FieldError) {
	correlationID := correlationIDFromContext(request.Context())
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("X-Correlation-ID", correlationID)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Problem{Type: "https://aquaos.dev/problems/" + code, Title: title, Status: status, Code: code, Detail: detail, Instance: request.URL.Path, CorrelationID: correlationID, Fields: fields})
}
