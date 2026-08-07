package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tylerkirby004-droid/aquaos/internal/alarms"
	"github.com/tylerkirby004-droid/aquaos/internal/config"
	"github.com/tylerkirby004-droid/aquaos/internal/devices"
	"github.com/tylerkirby004-droid/aquaos/internal/domain"
	"github.com/tylerkirby004-droid/aquaos/internal/equipment"
	"github.com/tylerkirby004-droid/aquaos/internal/output"
	"github.com/tylerkirby004-droid/aquaos/internal/sensors"
	"github.com/tylerkirby004-droid/aquaos/internal/state"
)

const correlationHeader = "X-Correlation-ID"

type correlationKey struct{}

// Dependencies contains only query and application-service boundaries used by HTTP.
type Dependencies struct {
	Devices       DeviceReader
	Sensors       SensorReader
	Equipment     EquipmentReader
	State         StateReader
	Commands      CommandService
	Alarms        AlarmService
	Configuration ConfigurationService
}

// DeviceReader is the API-owned device query contract.
type DeviceReader interface {
	Get(context.Context, domain.DeviceID) (domain.Device, error)
	List(context.Context) ([]domain.Device, error)
}

// SensorReader is the API-owned sensor query contract.
type SensorReader interface {
	Get(context.Context, domain.SensorID) (domain.Sensor, error)
}

// EquipmentReader is the API-owned equipment query contract.
type EquipmentReader interface {
	Get(context.Context, domain.EquipmentID) (domain.Equipment, error)
}

// StateReader is the API-owned canonical-state query contract.
type StateReader interface {
	Get(context.Context, state.Key) (state.Value, error)
	Snapshot(context.Context) (state.Snapshot, error)
}

// CommandService is the sole API command application-service contract.
type CommandService interface {
	Submit(context.Context, output.Command) (output.Result, error)
	Get(context.Context, domain.CommandID) (output.Result, error)
}

// AlarmService is the API alarm application-service contract.
type AlarmService interface {
	List(context.Context, alarms.Status) ([]alarms.Alarm, error)
	Acknowledge(context.Context, domain.AlarmID) (alarms.Alarm, error)
}

// ConfigurationService validates and atomically activates configuration.
type ConfigurationService interface {
	Current() config.Config
	Digest() string
	Plan(config.Config) (config.ReloadPlan, error)
	Reload(context.Context) error
}

type rateLimiter struct {
	mu          sync.Mutex
	rate, burst float64
	maximum     int
	clients     map[string]*bucket
}
type bucket struct {
	tokens  float64
	updated time.Time
}

func newRateLimiter(rate, burst int) *rateLimiter {
	if rate < 1 {
		rate = 1
	}
	if burst < 1 {
		burst = 1
	}
	return &rateLimiter{rate: float64(rate), burst: float64(burst), maximum: 1024, clients: make(map[string]*bucket)}
}
func (l *rateLimiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.clients[key]
	if !ok {
		if len(l.clients) >= l.maximum {
			oldestKey := ""
			var oldest time.Time
			for candidate, value := range l.clients {
				if oldestKey == "" || value.updated.Before(oldest) {
					oldestKey, oldest = candidate, value.updated
				}
			}
			delete(l.clients, oldestKey)
		}
		l.clients[key] = &bucket{tokens: l.burst - 1, updated: now}
		return true
	}
	b.tokens += now.Sub(b.updated).Seconds() * l.rate
	if b.tokens > l.burst {
		b.tokens = l.burst
	}
	b.updated = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func (s *Server) registerV1(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/health", s.authorized(RoleReader, false, http.HandlerFunc(s.details)))
	mux.Handle("GET /api/v1/system", s.authorized(RoleReader, false, http.HandlerFunc(s.system)))
	mux.Handle("GET /api/v1/devices", s.authorized(RoleReader, false, http.HandlerFunc(s.listDevices)))
	mux.Handle("GET /api/v1/devices/{id}", s.authorized(RoleReader, false, http.HandlerFunc(s.getDevice)))
	mux.Handle("GET /api/v1/state", s.authorized(RoleReader, false, http.HandlerFunc(s.getState)))
	mux.Handle("GET /api/v1/sensors/{id}/state", s.authorized(RoleReader, false, http.HandlerFunc(s.getSensorState)))
	mux.Handle("GET /api/v1/equipment/{id}/state", s.authorized(RoleReader, false, http.HandlerFunc(s.getEquipmentState)))
	mux.Handle("POST /api/v1/equipment/{id}/commands", s.authorized(RoleOperator, true, http.HandlerFunc(s.submitCommand)))
	mux.Handle("GET /api/v1/commands/{id}", s.authorized(RoleReader, false, http.HandlerFunc(s.getCommand)))
	mux.Handle("GET /api/v1/alarms", s.authorized(RoleReader, false, http.HandlerFunc(s.listAlarms)))
	mux.Handle("POST /api/v1/alarms/{id}/ack", s.authorized(RoleOperator, true, http.HandlerFunc(s.ackAlarm)))
	mux.Handle("POST /api/v1/config/validate", s.authorized(RoleAdministrator, true, http.HandlerFunc(s.validateConfig)))
	mux.Handle("POST /api/v1/config/reload", s.authorized(RoleAdministrator, true, http.HandlerFunc(s.reloadConfig)))
	mux.Handle("GET /api/v1/diagnostics", s.authorized(RoleAdministrator, false, http.HandlerFunc(s.diagnostics)))
}

func (s *Server) withCorrelation(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		id := request.Header.Get(correlationHeader)
		if domain.CorrelationID(id).Validate() != nil {
			generated, err := domain.NewCorrelationID()
			if err != nil {
				http.Error(w, "request identity unavailable", http.StatusInternalServerError)
				return
			}
			id = string(generated)
		}
		w.Header().Set(correlationHeader, id)
		next.ServeHTTP(w, request.WithContext(context.WithValue(request.Context(), correlationKey{}, id)))
	})
}
func correlationIDFromContext(ctx context.Context) string {
	value, _ := ctx.Value(correlationKey{}).(string)
	return value
}

func (s *Server) authorized(role Role, mutation bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		principal, err := s.authenticator.Authenticate(request)
		if err != nil {
			writeProblem(w, request, http.StatusUnauthorized, "authentication_required", "Authentication required", "Valid bearer credentials are required.")
			return
		}
		if err = s.authorizer.Authorize(request.Context(), principal, role); err != nil {
			writeProblem(w, request, http.StatusForbidden, "forbidden", "Forbidden", "The authenticated identity lacks the required role.")
			return
		}
		if mutation && !s.mutations.allow(principal.ID, time.Now()) {
			w.Header().Set("Retry-After", "1")
			writeProblem(w, request, http.StatusTooManyRequests, "rate_limited", "Too many requests", "The mutation rate limit was exceeded.")
			return
		}
		next.ServeHTTP(w, request.WithContext(context.WithValue(request.Context(), principalKey{}, principal)))
	})
}

func (s *Server) system(w http.ResponseWriter, request *http.Request) {
	if !s.require(s.dependencies.Configuration, w, request) {
		return
	}
	cfg := s.dependencies.Configuration.Current()
	version := "development"
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		version = info.Main.Version
	}
	writeJSON(w, http.StatusOK, map[string]any{"version": version, "goVersion": runtime.Version(), "site": cfg.MQTT.SiteID, "uptimeSeconds": int64(time.Since(s.startedAt).Seconds()), "configurationDigest": s.dependencies.Configuration.Digest()})
}

func (s *Server) listDevices(w http.ResponseWriter, request *http.Request) {
	if !s.require(s.dependencies.Devices, w, request) {
		return
	}
	values, err := s.dependencies.Devices.List(request.Context())
	if err != nil {
		s.internal(w, request, err)
		return
	}
	start, limit, ok := pagination(request, w)
	if !ok {
		return
	}
	if start > len(values) {
		start = len(values)
	}
	end := start + limit
	if end > len(values) {
		end = len(values)
	}
	next := ""
	if end < len(values) {
		next = base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(end)))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": values[start:end], "nextCursor": next})
}
func (s *Server) getDevice(w http.ResponseWriter, request *http.Request) {
	if !s.require(s.dependencies.Devices, w, request) {
		return
	}
	id := domain.DeviceID(request.PathValue("id"))
	if id.Validate() != nil {
		s.invalidID(w, request)
		return
	}
	value, err := s.dependencies.Devices.Get(request.Context(), id)
	if errors.Is(err, devices.ErrNotFound) {
		s.notFound(w, request)
		return
	}
	if err != nil {
		s.internal(w, request, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}
func (s *Server) getState(w http.ResponseWriter, request *http.Request) {
	if !s.require(s.dependencies.State, w, request) {
		return
	}
	snapshot, err := s.dependencies.State.Snapshot(request.Context())
	if err != nil {
		s.internal(w, request, err)
		return
	}
	if raw := request.URL.Query().Get("sinceRevision"); raw != "" {
		revision, parseErr := strconv.ParseUint(raw, 10, 64)
		if parseErr != nil {
			writeProblem(w, request, 400, "invalid_revision", "Invalid revision", "sinceRevision must be an unsigned integer.")
			return
		}
		if uint64(snapshot.Revision) <= revision {
			snapshot.Values = nil
		}
	}
	writeJSON(w, http.StatusOK, snapshot)
}
func (s *Server) getSensorState(w http.ResponseWriter, request *http.Request) {
	id := domain.SensorID(request.PathValue("id"))
	if id.Validate() != nil {
		s.invalidID(w, request)
		return
	}
	if !s.require(s.dependencies.Sensors, w, request) || !s.require(s.dependencies.State, w, request) {
		return
	}
	if _, err := s.dependencies.Sensors.Get(request.Context(), id); errors.Is(err, sensors.ErrNotFound) {
		s.notFound(w, request)
		return
	} else if err != nil {
		s.internal(w, request, err)
		return
	}
	value, err := s.dependencies.State.Get(request.Context(), state.Key{EntityKind: state.EntitySensor, EntityID: domain.EntityID(id), Plane: state.PlaneObservation, Attribute: "measurement"})
	if errors.Is(err, state.ErrNotFound) {
		s.notFound(w, request)
		return
	}
	if err != nil {
		s.internal(w, request, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}
func (s *Server) getEquipmentState(w http.ResponseWriter, request *http.Request) {
	id := domain.EquipmentID(request.PathValue("id"))
	if id.Validate() != nil {
		s.invalidID(w, request)
		return
	}
	if !s.require(s.dependencies.Equipment, w, request) || !s.require(s.dependencies.State, w, request) {
		return
	}
	item, err := s.dependencies.Equipment.Get(request.Context(), id)
	if errors.Is(err, equipment.ErrNotFound) {
		s.notFound(w, request)
		return
	}
	if err != nil {
		s.internal(w, request, err)
		return
	}
	snapshot, err := s.dependencies.State.Snapshot(request.Context())
	if err != nil {
		s.internal(w, request, err)
		return
	}
	values := make([]state.Value, 0)
	for _, value := range snapshot.Values {
		if value.Key.EntityKind == state.EntityEquipment && value.Key.EntityID == domain.EntityID(id) {
			values = append(values, value)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"equipment": item, "revision": snapshot.Revision, "values": values})
}

type commandRequest struct {
	On               bool             `json:"on"`
	ExpiresAt        time.Time        `json:"expiresAt"`
	ExpectedRevision *domain.Revision `json:"expectedRevision,omitempty"`
}

func (s *Server) submitCommand(w http.ResponseWriter, request *http.Request) {
	if !s.require(s.dependencies.Commands, w, request) {
		return
	}
	key := strings.TrimSpace(request.Header.Get("Idempotency-Key"))
	if key == "" || len(key) > 128 {
		writeProblem(w, request, 400, "idempotency_key_required", "Idempotency key required", "Idempotency-Key must contain 1 to 128 characters.")
		return
	}
	id := domain.EquipmentID(request.PathValue("id"))
	if id.Validate() != nil {
		s.invalidID(w, request)
		return
	}
	var body commandRequest
	if !s.decodeJSON(w, request, &body) {
		return
	}
	principal, _ := principalFromContext(request.Context())
	correlation := domain.CorrelationID(correlationIDFromContext(request.Context()))
	now := time.Now().UTC()
	result, err := s.dependencies.Commands.Submit(request.Context(), output.Command{IdempotencyKey: key, CorrelationID: correlation, EquipmentID: id, Requester: principal.ID, IssuedAt: now, ExpiresAt: body.ExpiresAt, ExpectedRevision: body.ExpectedRevision, On: body.On})
	if err != nil {
		if errors.Is(err, output.ErrConflict) {
			writeProblem(w, request, 409, "idempotency_conflict", "Idempotency conflict", "The key was already used for different command content.")
			return
		}
		s.invalid(w, request, err)
		return
	}
	if result.Reason == output.ReasonRevisionConflict {
		writeProblem(w, request, 409, "stale_revision", "Revision conflict", "The expected canonical-state revision is stale.")
		return
	}
	writeJSON(w, http.StatusAccepted, result)
}
func (s *Server) getCommand(w http.ResponseWriter, request *http.Request) {
	if !s.require(s.dependencies.Commands, w, request) {
		return
	}
	id := domain.CommandID(request.PathValue("id"))
	if id.Validate() != nil {
		s.invalidID(w, request)
		return
	}
	result, err := s.dependencies.Commands.Get(request.Context(), id)
	if errors.Is(err, output.ErrNotFound) {
		s.notFound(w, request)
		return
	}
	if err != nil {
		s.internal(w, request, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
func (s *Server) listAlarms(w http.ResponseWriter, request *http.Request) {
	if !s.require(s.dependencies.Alarms, w, request) {
		return
	}
	status := alarms.Status(request.URL.Query().Get("status"))
	values, err := s.dependencies.Alarms.List(request.Context(), status)
	if err != nil {
		s.internal(w, request, err)
		return
	}
	start, limit, ok := pagination(request, w)
	if !ok {
		return
	}
	if start > len(values) {
		start = len(values)
	}
	end := start + limit
	if end > len(values) {
		end = len(values)
	}
	next := ""
	if end < len(values) {
		next = base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(end)))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": values[start:end], "nextCursor": next})
}

type acknowledgementRequest struct {
	Reason           string          `json:"reason"`
	ExpectedRevision domain.Revision `json:"expectedRevision"`
}

func (s *Server) ackAlarm(w http.ResponseWriter, request *http.Request) {
	if !s.require(s.dependencies.Alarms, w, request) || !s.require(s.dependencies.State, w, request) {
		return
	}
	var body acknowledgementRequest
	if !s.decodeJSON(w, request, &body) {
		return
	}
	if strings.TrimSpace(body.Reason) == "" {
		writeProblem(w, request, 400, "reason_required", "Reason required", "An acknowledgement reason is required.")
		return
	}
	snapshot, err := s.dependencies.State.Snapshot(request.Context())
	if err != nil {
		s.internal(w, request, err)
		return
	}
	if snapshot.Revision != body.ExpectedRevision {
		writeProblem(w, request, 409, "stale_revision", "Revision conflict", "The expected canonical-state revision is stale.")
		return
	}
	id := domain.AlarmID(request.PathValue("id"))
	if id.Validate() != nil {
		s.invalidID(w, request)
		return
	}
	alarm, err := s.dependencies.Alarms.Acknowledge(request.Context(), id)
	if errors.Is(err, alarms.ErrNotFound) {
		s.notFound(w, request)
		return
	}
	if err != nil {
		s.invalid(w, request, err)
		return
	}
	s.logger.InfoContext(request.Context(), "alarm acknowledged through API", "alarm_id", id, "actor", mustPrincipal(request.Context()).ID, "reason", body.Reason, "correlation_id", correlationIDFromContext(request.Context()))
	writeJSON(w, http.StatusOK, alarm)
}

func (s *Server) validateConfig(w http.ResponseWriter, request *http.Request) {
	if !s.require(s.dependencies.Configuration, w, request) {
		return
	}
	payload, ok := s.readBody(w, request)
	if !ok {
		return
	}
	candidate, err := config.DecodeCandidate(payload)
	if err != nil {
		s.invalid(w, request, err)
		return
	}
	plan, err := s.dependencies.Configuration.Plan(candidate)
	if err != nil {
		var rejected *config.ReloadRejectedError
		if !errors.As(err, &rejected) {
			s.invalid(w, request, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"valid": true, "activatable": err == nil, "plan": plan})
}
func (s *Server) reloadConfig(w http.ResponseWriter, request *http.Request) {
	if !s.require(s.dependencies.Configuration, w, request) {
		return
	}
	if err := s.dependencies.Configuration.Reload(request.Context()); err != nil {
		s.invalid(w, request, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"reloaded": true, "digest": s.dependencies.Configuration.Digest()})
}
func (s *Server) diagnostics(w http.ResponseWriter, request *http.Request) {
	if !s.require(s.dependencies.Configuration, w, request) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"health": s.health.Report(), "configurationDigest": s.dependencies.Configuration.Digest(), "runtime": map[string]any{"goroutines": runtime.NumGoroutine(), "goVersion": runtime.Version()}, "redacted": true})
}

func pagination(request *http.Request, w http.ResponseWriter) (int, int, bool) {
	limit := 100
	if raw := request.URL.Query().Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 200 {
			writeProblem(w, request, 400, "invalid_pagination", "Invalid pagination", "limit must be between 1 and 200.")
			return 0, 0, false
		}
		limit = value
	}
	start := 0
	if raw := request.URL.Query().Get("cursor"); raw != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(raw)
		if err != nil {
			writeProblem(w, request, 400, "invalid_cursor", "Invalid cursor", "cursor is malformed.")
			return 0, 0, false
		}
		start, err = strconv.Atoi(string(decoded))
		if err != nil || start < 0 {
			writeProblem(w, request, 400, "invalid_cursor", "Invalid cursor", "cursor is malformed.")
			return 0, 0, false
		}
	}
	return start, limit, true
}
func (s *Server) decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, s.maximumBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		s.invalid(w, r, err)
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		s.invalid(w, r, errors.New("request must contain one JSON value"))
		return false
	}
	return true
}
func (s *Server) readBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	payload, err := io.ReadAll(io.LimitReader(r.Body, s.maximumBodyBytes+1))
	if err != nil {
		s.invalid(w, r, err)
		return nil, false
	}
	if int64(len(payload)) > s.maximumBodyBytes {
		s.invalid(w, r, errors.New("request body is too large"))
		return nil, false
	}
	return payload, true
}
func (s *Server) require(value any, w http.ResponseWriter, r *http.Request) bool {
	if value == nil {
		writeProblem(w, r, 503, "service_unavailable", "Service unavailable", "The requested application service is not configured.")
		return false
	}
	return true
}
func (s *Server) invalidID(w http.ResponseWriter, r *http.Request) {
	writeProblem(w, r, 400, "invalid_id", "Invalid identifier", "The path identifier must be a canonical UUID.")
}
func (s *Server) invalid(w http.ResponseWriter, r *http.Request, err error) {
	s.logger.WarnContext(r.Context(), "API request rejected", "path", r.URL.Path, "error", err, "correlation_id", correlationIDFromContext(r.Context()))
	writeProblem(w, r, 400, "invalid_request", "Invalid request", err.Error())
}
func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	writeProblem(w, r, 404, "not_found", "Not found", "The requested resource does not exist.")
}
func (s *Server) internal(w http.ResponseWriter, r *http.Request, err error) {
	s.logger.ErrorContext(r.Context(), "API request failed", "path", r.URL.Path, "error", err, "correlation_id", correlationIDFromContext(r.Context()))
	writeProblem(w, r, 500, "internal_error", "Internal error", "The request could not be completed.")
}
func mustPrincipal(ctx context.Context) Principal {
	principal, _ := principalFromContext(ctx)
	return principal
}
