// Package firstboot provides the one-time browser boundary used by appliance images.
package firstboot

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
)

var (
	sitePattern     = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
	timezonePattern = regexp.MustCompile(`^[A-Za-z0-9_+/-]{1,64}$`)
)

// InstallRequest contains the small set of choices required before installation.
type InstallRequest struct {
	SiteID                string `json:"siteId"`
	Address               string `json:"address"`
	Timezone              string `json:"timezone"`
	AdvancedHistory       bool   `json:"advancedHistory"`
	DedicatedAppliance    bool   `json:"dedicatedAppliance"`
	IndependentSafeguards bool   `json:"independentSafeguards"`
}

// Runner performs the privileged, signed appliance installation.
type Runner interface {
	Install(context.Context, InstallRequest) (InstallResult, error)
}

// InstallResult contains the one-time browser handoff to AquaOS Admin.
type InstallResult struct {
	AdminAccessToken string
}

// Server authenticates and validates one-time browser installation requests.
type Server struct {
	token   string
	runner  Runner
	ctx     context.Context
	address string
	mux     *http.ServeMux
	mu      sync.Mutex
	running bool
}

// NewServer creates a first-boot server with no ambient global state.
func NewServer(ctx context.Context, token, suggestedAddress string, runner Runner) (*Server, error) {
	if ctx == nil {
		return nil, errors.New("first-boot context is required")
	}
	if len(token) < 16 {
		return nil, errors.New("setup token must contain at least 16 characters")
	}
	if runner == nil {
		return nil, errors.New("first-boot runner is required")
	}
	if suggestedAddress != "" {
		ip := net.ParseIP(suggestedAddress)
		if ip == nil || ip.To4() == nil || !ip.IsPrivate() {
			return nil, errors.New("suggested address must be a private IPv4 address")
		}
	}
	server := &Server{token: token, runner: runner, ctx: ctx, address: suggestedAddress, mux: http.NewServeMux()}
	server.mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	server.mux.HandleFunc("GET /api/network", server.network)
	server.mux.HandleFunc("GET /", server.index)
	server.mux.HandleFunc("POST /api/install", server.install)
	return server, nil
}

// Handler returns the server's HTTP handler.
func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) index(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(indexHTML))
}

func (s *Server) network(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]string{"address": s.address})
}

func (s *Server) install(w http.ResponseWriter, r *http.Request) {
	provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if len(provided) != len(s.token) || subtle.ConstantTimeCompare([]byte(provided), []byte(s.token)) != 1 {
		http.Error(w, "invalid setup code", http.StatusUnauthorized)
		return
	}
	var request InstallRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		http.Error(w, "invalid installation request", http.StatusBadRequest)
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		http.Error(w, "installation request must contain one JSON object", http.StatusBadRequest)
		return
	}
	if err := Validate(request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		http.Error(w, "installation is already running", http.StatusConflict)
		return
	}
	s.running = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()
	result, err := s.runner.Install(s.ctx, request)
	if err != nil {
		http.Error(w, "installation failed; inspect the local system log", http.StatusInternalServerError)
		return
	}
	if len(result.AdminAccessToken) < 32 {
		http.Error(w, "installation completed without a valid Admin handoff", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = fmt.Fprintf(w, `{"adminUrl":"https://%s:8090/admin/#access_token=%s"}`, request.Address, result.AdminAccessToken)
}

// Validate rejects unsafe or ambiguous first-boot choices.
func Validate(request InstallRequest) error {
	if !request.DedicatedAppliance || !request.IndependentSafeguards {
		return errors.New("both safety acknowledgements are required")
	}
	if !sitePattern.MatchString(request.SiteID) {
		return errors.New("site ID must start with a letter and use lowercase letters, numbers, or hyphens")
	}
	if !timezonePattern.MatchString(request.Timezone) {
		return errors.New("timezone contains unsupported characters")
	}
	ip := net.ParseIP(request.Address)
	if ip == nil || ip.To4() == nil || !ip.IsPrivate() {
		return errors.New("address must be a private IPv4 address")
	}
	return nil
}

const indexHTML = `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Install AquaOS</title><style>body{font:18px system-ui;max-width:720px;margin:3rem auto;padding:0 1rem;background:#071b26;color:#e8f6fa}fieldset{border:1px solid #39758b;border-radius:12px;padding:1.2rem}label{display:block;margin:1rem 0}input[type=text],input[type=password]{width:100%;box-sizing:border-box;padding:.7rem}button{padding:.8rem 1.2rem;background:#2ec4b6;border:0;border-radius:8px;font-weight:700}.warning{color:#ffd166}#status{white-space:pre-wrap}</style></head><body><h1>Welcome to AquaOS</h1><p>This installs AquaOS on this dedicated computer. It will not activate aquarium equipment.</p><fieldset><label>Setup code shown on the computer<input id="token" type="password" maxlength="19" autocomplete="one-time-code" placeholder="xxxx-xxxx-xxxx-xxxx"></label><label>Tank/site name<input id="site" type="text" value="home-reef"></label><label>Detected AquaOS address<input id="address" type="text" readonly></label><label>Timezone<input id="timezone" type="text" value="America/Chicago"></label><label><input id="history" type="checkbox"> Advanced long-term history with InfluxDB and Grafana</label><label class="warning"><input id="dedicated" type="checkbox"> I confirm this computer is dedicated to AquaOS.</label><label class="warning"><input id="safeguards" type="checkbox"> I will use independent physical equipment safeguards.</label><button id="install">Install AquaOS</button><p id="status"></p></fieldset><script>const byId=id=>document.getElementById(id);fetch('/api/network').then(r=>r.json()).then(n=>{if(n.address)byId('address').value=n.address});byId('install').onclick=async()=>{const s=byId('status');s.textContent='Installing. Keep this page open…';const token=byId('token').value.toLowerCase().replace(/[^0-9a-f]/g,'');const body={siteId:byId('site').value,address:byId('address').value,timezone:byId('timezone').value,advancedHistory:byId('history').checked,dedicatedAppliance:byId('dedicated').checked,independentSafeguards:byId('safeguards').checked};try{const r=await fetch('/api/install',{method:'POST',headers:{Authorization:'Bearer '+token,'Content-Type':'application/json'},body:JSON.stringify(body)});if(!r.ok)throw new Error(await r.text());const result=await r.json();s.textContent='Installation complete. Opening AquaOS Admin…';location.href=result.adminUrl}catch(e){s.textContent=e.message}}</script></body></html>`
