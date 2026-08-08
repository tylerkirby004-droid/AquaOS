// Package admin provides the non-authoritative recovery and deployment GUI.
// Every mutation calls the same operations application service as aquaosctl.
package admin

import (
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/tylerkirby004-droid/aquaos/internal/config"
	"github.com/tylerkirby004-droid/aquaos/internal/discovery"
	"github.com/tylerkirby004-droid/aquaos/internal/health"
	"github.com/tylerkirby004-droid/aquaos/internal/operations"
	"gopkg.in/yaml.v3"
)

//go:embed web/*
var assets embed.FS

const adminSessionCookie = "aquaos_admin_session"

// Operations is the Admin GUI-owned application-service contract.
type Operations interface {
	GetStatus(context.Context, operations.Actor) (operations.Status, error)
	GetConfiguration(context.Context, operations.Actor) (config.Config, error)
	Verify(context.Context, operations.Actor) (operations.Diagnostics, error)
	Repair(context.Context, operations.Actor, bool) (operations.Result, error)
	Install(context.Context, operations.InstallRequest) (operations.Result, error)
	Upgrade(context.Context, operations.UpgradeRequest) (operations.Result, error)
	Rollback(context.Context, operations.Actor, bool) (operations.Result, error)
	Backup(context.Context, operations.Actor) ([]byte, error)
	Restore(context.Context, operations.Actor, []byte, bool) (operations.Result, error)
	Uninstall(context.Context, operations.Actor, bool, bool) (operations.Result, error)
	ValidateConfiguration(context.Context, operations.Actor, []byte) (operations.ConfigurationResult, error)
	ApplyConfiguration(context.Context, operations.Actor, []byte, bool) (operations.ConfigurationResult, error)
}

// Discovery is the Admin-owned read-only endpoint probing contract.
type Discovery interface {
	Probe(context.Context, []discovery.Candidate) ([]discovery.Result, error)
}

// RuntimeHealth reads the authoritative Core health report without exposing
// Core credentials to the browser.
type RuntimeHealth interface {
	Report(context.Context) (any, error)
}

// Option configures optional Admin capabilities.
type Option func(*Server)

// WithDiscovery enables bounded, read-only Shelly and ESP32 probing.
func WithDiscovery(service Discovery) Option {
	return func(server *Server) { server.discovery = service }
}

// WithRuntimeHealth enables the read-only operational status panel.
func WithRuntimeHealth(service RuntimeHealth) Option {
	return func(server *Server) { server.runtimeHealth = service }
}

// Config contains externally supplied listener and request bounds.
type Config struct {
	Address             string
	Token               string
	MaximumRequestBytes int64
	ShutdownTimeout     time.Duration
	AuthenticationRate  int
	AuthenticationBurst int
	MutationRate        int
	MutationBurst       int
	TLSCertificateFile  string
	TLSKeyFile          string
	// TrustedIngress delegates browser authentication to a trusted reverse
	// proxy such as Home Assistant Ingress. The listener must not be exposed
	// directly when this is enabled.
	TrustedIngress      bool
	// TrustedIngressCIDR is the exact reverse-proxy network allowed to use
	// delegated authentication.
	TrustedIngressCIDR  string
}

// Server owns the authenticated Admin GUI listener.
type Server struct {
	cfg              Config
	operations       Operations
	logger           *slog.Logger
	server           *http.Server
	mu               sync.RWMutex
	running          bool
	serveErr         error
	cancel           context.CancelFunc
	done             chan struct{}
	cancellationDone chan struct{}
	authentication   *requestLimiter
	mutations        *requestLimiter
	discovery        Discovery
	runtimeHealth    RuntimeHealth
	sessionToken     string
	secureCookie     bool
	trustedIngress   *net.IPNet
}

// New constructs an Admin GUI server without starting it.
func New(cfg Config, service Operations, logger *slog.Logger, options ...Option) (*Server, error) {
	if cfg.Address == "" || (!cfg.TrustedIngress && len(cfg.Token) < 32) || cfg.MaximumRequestBytes < 1024 || cfg.MaximumRequestBytes > 64*1024*1024 || cfg.ShutdownTimeout <= 0 || cfg.AuthenticationRate < 1 || cfg.AuthenticationBurst < 1 || cfg.MutationRate < 1 || cfg.MutationBurst < 1 {
		return nil, errors.New("admin listener, token, and safe bounds are required")
	}
	if service == nil || logger == nil {
		return nil, errors.New("admin operations and logger are required")
	}
	if (cfg.TLSCertificateFile == "") != (cfg.TLSKeyFile == "") {
		return nil, errors.New("admin TLS certificate and key must be supplied together")
	}
	var trustedIngress *net.IPNet
	if cfg.TrustedIngress {
		_, network, parseErr := net.ParseCIDR(cfg.TrustedIngressCIDR)
		if parseErr != nil {
			return nil, errors.New("trusted ingress requires an explicit proxy CIDR")
		}
		trustedIngress = network
	}
	web, err := fs.Sub(assets, "web")
	if err != nil {
		return nil, err
	}
	sessionSecret := cfg.Token
	if cfg.TrustedIngress {
		sessionSecret = "home-assistant-ingress"
	}
	mac := hmac.New(sha256.New, []byte(sessionSecret))
	_, _ = mac.Write([]byte("aquaos-admin-browser-session-v1"))
	result := &Server{cfg: cfg, operations: service, logger: logger, authentication: newRequestLimiter(cfg.AuthenticationRate, cfg.AuthenticationBurst, 1024), mutations: newRequestLimiter(cfg.MutationRate, cfg.MutationBurst, 1024), sessionToken: hex.EncodeToString(mac.Sum(nil)), secureCookie: cfg.TLSCertificateFile != "", trustedIngress: trustedIngress}
	for _, option := range options {
		option(result)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, map[string]string{"status": "alive"}) })
	mux.Handle("GET /admin/", http.StripPrefix("/admin/", http.FileServer(http.FS(web))))
	mux.HandleFunc("POST /api/session", result.createSession)
	mux.HandleFunc("GET /api/session", result.sessionStatus)
	mux.Handle("DELETE /api/session", result.authorize(http.HandlerFunc(result.deleteSession)))
	mux.Handle("GET /api/status", result.authorize(http.HandlerFunc(result.status)))
	mux.Handle("GET /api/config", result.authorize(http.HandlerFunc(result.configuration)))
	mux.Handle("GET /api/runtime", result.authorize(http.HandlerFunc(result.runtime)))
	mux.Handle("POST /api/config/editable/validate", result.authorize(http.HandlerFunc(result.validateEditableConfiguration)))
	mux.Handle("POST /api/config/editable/apply", result.authorize(http.HandlerFunc(result.applyEditableConfiguration)))
	mux.Handle("POST /api/discovery/probe", result.authorize(http.HandlerFunc(result.probe)))
	mux.Handle("POST /api/verify", result.authorize(http.HandlerFunc(result.verify)))
	mux.Handle("POST /api/repair", result.authorize(http.HandlerFunc(result.repair)))
	mux.Handle("POST /api/install", result.authorize(http.HandlerFunc(result.install)))
	mux.Handle("POST /api/upgrade", result.authorize(http.HandlerFunc(result.upgrade)))
	mux.Handle("POST /api/rollback", result.authorize(http.HandlerFunc(result.rollback)))
	mux.Handle("GET /api/backup", result.authorize(http.HandlerFunc(result.backup)))
	mux.Handle("POST /api/restore", result.authorize(http.HandlerFunc(result.restore)))
	mux.Handle("POST /api/uninstall", result.authorize(http.HandlerFunc(result.uninstall)))
	mux.Handle("POST /api/config/validate", result.authorize(http.HandlerFunc(result.validateConfiguration)))
	mux.Handle("POST /api/config/apply", result.authorize(http.HandlerFunc(result.applyConfiguration)))
	result.server = &http.Server{Addr: cfg.Address, Handler: result.secure(mux), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 * 1024}
	return result, nil
}

// Name returns the component name.
func (*Server) Name() string { return "admin" }

// Start starts explicitly owned serving work.
func (s *Server) Start(ctx context.Context) error {
	var tlsConfiguration *tls.Config
	if s.cfg.TLSCertificateFile != "" {
		certificate, err := tls.LoadX509KeyPair(s.cfg.TLSCertificateFile, s.cfg.TLSKeyFile)
		if err != nil {
			return fmt.Errorf("load admin TLS identity: %w", err)
		}
		tlsConfiguration = &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12}
	}
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", s.server.Addr)
	if err != nil {
		return err
	}
	runCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.running = true
	s.cancel = cancel
	s.done = make(chan struct{})
	s.cancellationDone = make(chan struct{})
	done := s.done
	cancellationDone := s.cancellationDone
	s.mu.Unlock()
	go func() {
		defer close(done)
		serveListener := listener
		if tlsConfiguration != nil {
			serveListener = tls.NewListener(listener, tlsConfiguration)
		}
		err := s.server.Serve(serveListener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.mu.Lock()
			s.serveErr = err
			s.running = false
			s.mu.Unlock()
		}
	}()
	go func() {
		defer close(cancellationDone)
		<-runCtx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), s.cfg.ShutdownTimeout)
		defer shutdownCancel()
		_ = s.server.Shutdown(shutdownCtx)
	}()
	return nil
}

// Stop cancels, shuts down, and joins serving work.
func (s *Server) Stop(ctx context.Context) error {
	s.mu.RLock()
	cancel, done, cancellationDone := s.cancel, s.done, s.cancellationDone
	s.mu.RUnlock()
	if cancel != nil {
		cancel()
	}
	err := s.server.Shutdown(ctx)
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return errors.Join(err, ctx.Err())
		}
	}
	if cancellationDone != nil {
		select {
		case <-cancellationDone:
		case <-ctx.Done():
			return errors.Join(err, ctx.Err())
		}
	}
	s.mu.Lock()
	s.running = false
	s.mu.Unlock()
	return err
}

// Health reports Admin GUI listener health without affecting Core.
func (s *Server) Health() health.Status {
	s.mu.RLock()
	running, serveErr := s.running, s.serveErr
	s.mu.RUnlock()
	state := health.StateUnhealthy
	message := ""
	if running && serveErr == nil {
		state = health.StateHealthy
	}
	if serveErr != nil {
		message = serveErr.Error()
	}
	return health.NewStatus(s.Name(), state, message, time.Now().UTC())
}

func (s *Server) authorize(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := remoteKey(r)
		if !s.authentication.allow(key, time.Now()) {
			w.Header().Set("Retry-After", "1")
			writeProblem(w, http.StatusTooManyRequests, "rate_limited", "Authentication rate limit exceeded.")
			return
		}
		if !s.authenticated(r) {
			writeProblem(w, 401, "authentication_required", "Valid bearer credentials are required.")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && !s.mutations.allow(key, time.Now()) {
			w.Header().Set("Retry-After", "1")
			writeProblem(w, http.StatusTooManyRequests, "rate_limited", "Mutation rate limit exceeded.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) authenticated(r *http.Request) bool {
	if s.cfg.TrustedIngress {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		return err == nil && s.trustedIngress.Contains(net.ParseIP(host))
	}
	provided := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if len(provided) == len(s.cfg.Token) && subtle.ConstantTimeCompare([]byte(provided), []byte(s.cfg.Token)) == 1 {
		return true
	}
	cookie, err := r.Cookie(adminSessionCookie)
	return err == nil && len(cookie.Value) == len(s.sessionToken) && subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(s.sessionToken)) == 1
}

func (s *Server) createSession(w http.ResponseWriter, r *http.Request) {
	if s.cfg.TrustedIngress {
		if !s.authenticated(r) {
			writeProblem(w, http.StatusUnauthorized, "authentication_required", "The request did not arrive through Home Assistant Ingress.")
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	key := remoteKey(r)
	if !s.authentication.allow(key, time.Now()) {
		w.Header().Set("Retry-After", "1")
		writeProblem(w, http.StatusTooManyRequests, "rate_limited", "Authentication rate limit exceeded.")
		return
	}
	provided := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if len(provided) != len(s.cfg.Token) || subtle.ConstantTimeCompare([]byte(provided), []byte(s.cfg.Token)) != 1 {
		writeProblem(w, http.StatusUnauthorized, "authentication_required", "Valid pairing credentials are required.")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: adminSessionCookie, Value: s.sessionToken, Path: "/", MaxAge: 365 * 24 * 60 * 60, HttpOnly: true, Secure: s.secureCookie, SameSite: http.SameSiteStrictMode})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) sessionStatus(w http.ResponseWriter, r *http.Request) {
	if !s.authenticated(r) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if s.cfg.TrustedIngress {
		w.Header().Set("X-AquaOS-Authentication", "home-assistant-ingress")
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteSession(w http.ResponseWriter, _ *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: adminSessionCookie, Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.secureCookie, SameSite: http.SameSiteStrictMode})
	w.WriteHeader(http.StatusNoContent)
}
func (s *Server) secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		frameAncestors := "'none'"
		if s.cfg.TrustedIngress {
			frameAncestors = "'self'"
		}
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors "+frameAncestors)
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if !s.cfg.TrustedIngress {
			w.Header().Set("X-Frame-Options", "DENY")
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			origin := r.Header.Get("Origin")
			if origin != "" && origin != "http://"+r.Host && origin != "https://"+r.Host {
				writeProblem(w, http.StatusForbidden, "origin_rejected", "Cross-origin mutation requests are forbidden.")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
func actor() operations.Actor { return operations.Actor{ID: "admin-gui", Administrator: true} }
func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	value, err := s.operations.GetStatus(r.Context(), actor())
	s.respond(w, value, err)
}
func (s *Server) runtime(w http.ResponseWriter, r *http.Request) {
	if s.runtimeHealth == nil {
		writeProblem(w, http.StatusServiceUnavailable, "runtime_unavailable", "AquaOS Core status is unavailable.")
		return
	}
	value, err := s.runtimeHealth.Report(r.Context())
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"available": false, "state": "unavailable", "message": "AquaOS Core is not responding. Run the system checks and use guided repair."})
		return
	}
	s.respond(w, value, nil)
}
func (s *Server) configuration(w http.ResponseWriter, r *http.Request) {
	value, err := s.operations.GetConfiguration(r.Context(), actor())
	if err != nil {
		s.operationFailed(w, err)
		return
	}
	s.respond(w, editableConfiguration{Configuration: value, HTTPBearerTokenFile: value.HTTP.BearerTokenFile, InfluxDBTokenFile: value.Storage.InfluxDB.TokenFile}, nil)
}

func (s *Server) probe(w http.ResponseWriter, r *http.Request) {
	if s.discovery == nil {
		writeProblem(w, http.StatusServiceUnavailable, "discovery_unavailable", "Device discovery is not enabled.")
		return
	}
	var request struct {
		Candidates []discovery.Candidate `json:"candidates"`
	}
	if !s.decode(w, r, &request) {
		return
	}
	value, err := s.discovery.Probe(r.Context(), request.Candidates)
	s.respond(w, value, err)
}

type editableConfiguration struct {
	Configuration       config.Config `json:"configuration"`
	HTTPBearerTokenFile string        `json:"httpBearerTokenFile"`
	InfluxDBTokenFile   string        `json:"influxdbTokenFile"`
}

func (s *Server) validateEditableConfiguration(w http.ResponseWriter, r *http.Request) {
	s.processEditableConfiguration(w, r, false)
}

func (s *Server) applyEditableConfiguration(w http.ResponseWriter, r *http.Request) {
	s.processEditableConfiguration(w, r, true)
}

func (s *Server) processEditableConfiguration(w http.ResponseWriter, r *http.Request, apply bool) {
	var request editableConfiguration
	if !s.decode(w, r, &request) {
		return
	}
	request.Configuration.HTTP.BearerTokenFile = request.HTTPBearerTokenFile
	request.Configuration.Storage.InfluxDB.TokenFile = request.InfluxDBTokenFile
	payload, err := yaml.Marshal(request.Configuration)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_configuration", "Configuration could not be encoded.")
		return
	}
	if apply {
		value, operationErr := s.operations.ApplyConfiguration(r.Context(), actor(), payload, false)
		s.respond(w, value, operationErr)
		return
	}
	value, operationErr := s.operations.ValidateConfiguration(r.Context(), actor(), payload)
	s.respond(w, value, operationErr)
}
func (s *Server) verify(w http.ResponseWriter, r *http.Request) {
	value, err := s.operations.Verify(r.Context(), actor())
	if err != nil && value.Checks != nil {
		s.logger.Warn("Admin verification found failed checks")
		writeJSON(w, http.StatusOK, value)
		return
	}
	s.respond(w, value, err)
}

type dryRunRequest struct {
	DryRun bool `json:"dryRun"`
}

func (s *Server) repair(w http.ResponseWriter, r *http.Request) {
	var request dryRunRequest
	if !s.decode(w, r, &request) {
		return
	}
	value, err := s.operations.Repair(r.Context(), actor(), request.DryRun)
	s.respond(w, value, err)
}
func (s *Server) rollback(w http.ResponseWriter, r *http.Request) {
	var request dryRunRequest
	if !s.decode(w, r, &request) {
		return
	}
	value, err := s.operations.Rollback(r.Context(), actor(), request.DryRun)
	s.respond(w, value, err)
}

type installRequest struct {
	Version               string `json:"version"`
	Binary                string `json:"binaryBase64"`
	SHA256                string `json:"sha256"`
	Signature             string `json:"signatureHex"`
	PublicKey             string `json:"publicKeyHex"`
	Configuration         string `json:"configurationBase64"`
	ControlVMAcknowledged bool   `json:"controlVmAcknowledged"`
	DryRun                bool   `json:"dryRun"`
}

func (s *Server) install(w http.ResponseWriter, r *http.Request) {
	var request installRequest
	if !s.decode(w, r, &request) {
		return
	}
	binary, signature, key, configuration, err := decodeArtifact(request.Binary, request.Signature, request.PublicKey, request.Configuration)
	if err != nil {
		writeProblem(w, 400, "invalid_artifact", err.Error())
		return
	}
	value, err := s.operations.Install(r.Context(), operations.InstallRequest{Actor: actor(), Version: request.Version, Binary: binary, SHA256: request.SHA256, Signature: signature, PublicKey: key, Configuration: configuration, ControlVMAcknowledged: request.ControlVMAcknowledged, DryRun: request.DryRun})
	s.respond(w, value, err)
}

type upgradeRequest struct {
	Version   string `json:"version"`
	Binary    string `json:"binaryBase64"`
	SHA256    string `json:"sha256"`
	Signature string `json:"signatureHex"`
	PublicKey string `json:"publicKeyHex"`
	DryRun    bool   `json:"dryRun"`
}

func (s *Server) upgrade(w http.ResponseWriter, r *http.Request) {
	var request upgradeRequest
	if !s.decode(w, r, &request) {
		return
	}
	binary, signature, key, _, err := decodeArtifact(request.Binary, request.Signature, request.PublicKey, "")
	if err != nil {
		writeProblem(w, 400, "invalid_artifact", err.Error())
		return
	}
	value, err := s.operations.Upgrade(r.Context(), operations.UpgradeRequest{Actor: actor(), Version: request.Version, Binary: binary, SHA256: request.SHA256, Signature: signature, PublicKey: key, DryRun: request.DryRun})
	s.respond(w, value, err)
}
func (s *Server) backup(w http.ResponseWriter, r *http.Request) {
	payload, err := s.operations.Backup(r.Context(), actor())
	if err != nil {
		s.operationFailed(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename=aquaos-backup.zip")
	w.WriteHeader(200)
	_, _ = w.Write(payload)
}
func (s *Server) restore(w http.ResponseWriter, r *http.Request) {
	dryRun := r.URL.Query().Get("dryRun") == "true"
	payload, err := io.ReadAll(io.LimitReader(r.Body, s.cfg.MaximumRequestBytes+1))
	if err != nil || int64(len(payload)) > s.cfg.MaximumRequestBytes {
		writeProblem(w, 400, "invalid_backup", "Backup exceeds the configured limit.")
		return
	}
	value, err := s.operations.Restore(r.Context(), actor(), payload, dryRun)
	s.respond(w, value, err)
}

type uninstallRequest struct {
	PreserveData bool `json:"preserveData"`
	DryRun       bool `json:"dryRun"`
}

func (s *Server) uninstall(w http.ResponseWriter, r *http.Request) {
	var request uninstallRequest
	if !s.decode(w, r, &request) {
		return
	}
	value, err := s.operations.Uninstall(r.Context(), actor(), request.PreserveData, request.DryRun)
	s.respond(w, value, err)
}
func (s *Server) validateConfiguration(w http.ResponseWriter, r *http.Request) {
	payload, ok := s.readPayload(w, r)
	if !ok {
		return
	}
	value, err := s.operations.ValidateConfiguration(r.Context(), actor(), payload)
	s.respond(w, value, err)
}
func (s *Server) applyConfiguration(w http.ResponseWriter, r *http.Request) {
	payload, ok := s.readPayload(w, r)
	if !ok {
		return
	}
	value, err := s.operations.ApplyConfiguration(r.Context(), actor(), payload, r.URL.Query().Get("dryRun") == "true")
	s.respond(w, value, err)
}
func (s *Server) readPayload(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	payload, err := io.ReadAll(io.LimitReader(r.Body, s.cfg.MaximumRequestBytes+1))
	if err != nil || int64(len(payload)) > s.cfg.MaximumRequestBytes {
		writeProblem(w, 400, "invalid_payload", "Payload exceeds the configured limit.")
		return nil, false
	}
	return payload, true
}
func (s *Server) decode(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, s.cfg.MaximumRequestBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeProblem(w, 400, "invalid_request", "Request JSON is invalid.")
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeProblem(w, 400, "invalid_request", "Only one JSON value is allowed.")
		return false
	}
	return true
}
func decodeArtifact(binaryValue, signatureValue, keyValue, configurationValue string) ([]byte, []byte, ed25519.PublicKey, []byte, error) {
	binary, err := base64.StdEncoding.DecodeString(binaryValue)
	if err != nil {
		return nil, nil, nil, nil, errors.New("binaryBase64 is invalid")
	}
	signature, err := hex.DecodeString(signatureValue)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return nil, nil, nil, nil, errors.New("signatureHex is invalid")
	}
	key, err := hex.DecodeString(keyValue)
	if err != nil || len(key) != ed25519.PublicKeySize {
		return nil, nil, nil, nil, errors.New("publicKeyHex is invalid")
	}
	configuration := []byte(nil)
	if configurationValue != "" {
		configuration, err = base64.StdEncoding.DecodeString(configurationValue)
		if err != nil {
			return nil, nil, nil, nil, errors.New("configurationBase64 is invalid")
		}
	}
	return binary, signature, ed25519.PublicKey(key), configuration, nil
}
func (s *Server) respond(w http.ResponseWriter, value any, err error) {
	if err != nil {
		s.operationFailed(w, err)
		return
	}
	writeJSON(w, 200, value)
}
func (s *Server) operationFailed(w http.ResponseWriter, err error) {
	s.logger.Warn("Admin operation rejected", "error_type", fmt.Sprintf("%T", err))
	writeProblem(w, http.StatusBadRequest, "operation_failed", "The requested operation was rejected. Review server diagnostics for details.")
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeProblem(w http.ResponseWriter, status int, code, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"type": "https://aquaos.dev/problems/" + code, "title": "Admin operation failed", "status": status, "code": code, "detail": detail})
}
