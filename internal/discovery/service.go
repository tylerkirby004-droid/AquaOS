// Package discovery performs bounded, read-only probing of operator-approved
// LAN endpoints. It never scans arbitrary address ranges or issues commands.
package discovery

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tylerkirby004-droid/aquaos/internal/adapters/esp32"
	"github.com/tylerkirby004-droid/aquaos/internal/adapters/shelly"
)

const maximumCandidates = 64

// Kind identifies a supported direct-LAN adapter.
type Kind string

//nolint:revive // Kind values are documented collectively by Kind.
const (
	KindShelly Kind = "shelly"
	KindESP32  Kind = "esp32"
)

// Candidate is an operator-approved address to probe without mutation.
type Candidate struct {
	Kind        Kind   `json:"kind"`
	BaseURL     string `json:"baseUrl"`
	Channel     int    `json:"channel,omitempty"`
	BearerToken string `json:"-"`
}

// Result is a protocol-neutral discovery snapshot suitable for mapping UI.
type Result struct {
	Kind         Kind     `json:"kind"`
	BaseURL      string   `json:"baseUrl"`
	Channel      int      `json:"channel,omitempty"`
	Reachable    bool     `json:"reachable"`
	Identity     string   `json:"identity,omitempty"`
	Firmware     string   `json:"firmware,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
	ProbeIDs     []string `json:"probeIds,omitempty"`
	Message      string   `json:"message,omitempty"`
}

// ShellyClient is the read-only client subset consumed by discovery.
type ShellyClient interface {
	GetSwitchStatus(context.Context, string, int) (shelly.SwitchStatus, error)
	GetSwitchConfig(context.Context, string, int) (shelly.SwitchConfig, error)
}

// ESP32Client is the read-only client subset consumed by discovery.
type ESP32Client interface {
	Snapshot(context.Context, string, string) (esp32.SnapshotDTO, error)
}

// Service owns bounded synchronous discovery work.
type Service struct {
	shelly      ShellyClient
	esp32       ESP32Client
	concurrency int
	timeout     time.Duration
}

// New constructs a discovery service with explicit resource bounds.
func New(shellyClient ShellyClient, esp32Client ESP32Client, concurrency int, timeout time.Duration) (*Service, error) {
	if shellyClient == nil || esp32Client == nil {
		return nil, errors.New("discovery clients are required")
	}
	if concurrency < 1 || concurrency > 16 || timeout <= 0 || timeout > 30*time.Second {
		return nil, errors.New("discovery concurrency or timeout is outside safe bounds")
	}
	return &Service{shelly: shellyClient, esp32: esp32Client, concurrency: concurrency, timeout: timeout}, nil
}

// Probe checks only the supplied candidates, preserves one result per input,
// and joins all cancellable workers before returning.
func (s *Service) Probe(ctx context.Context, candidates []Candidate) ([]Result, error) {
	if len(candidates) == 0 || len(candidates) > maximumCandidates {
		return nil, fmt.Errorf("candidate count must be between 1 and %d", maximumCandidates)
	}
	results := make([]Result, len(candidates))
	semaphore := make(chan struct{}, s.concurrency)
	var group sync.WaitGroup
	for index, candidate := range candidates {
		if err := validateCandidate(candidate); err != nil {
			return nil, fmt.Errorf("candidate %d is invalid", index)
		}
		group.Add(1)
		go func(position int, value Candidate) {
			defer group.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				results[position] = Result{Kind: value.Kind, BaseURL: value.BaseURL, Channel: value.Channel, Message: "cancelled"}
				return
			}
			probeCtx, cancel := context.WithTimeout(ctx, s.timeout)
			defer cancel()
			results[position] = s.probe(probeCtx, value)
		}(index, candidate)
	}
	group.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Kind == results[j].Kind {
			return results[i].BaseURL < results[j].BaseURL
		}
		return results[i].Kind < results[j].Kind
	})
	return results, nil
}

func validateCandidate(candidate Candidate) error {
	if candidate.Kind != KindShelly && candidate.Kind != KindESP32 {
		return errors.New("unsupported candidate kind")
	}
	parsed, err := url.Parse(strings.TrimSpace(candidate.BaseURL))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("candidate URL must be an HTTP or HTTPS origin without credentials")
	}
	host := strings.ToLower(parsed.Hostname())
	ip := net.ParseIP(host)
	if ip != nil {
		if !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() {
			return errors.New("candidate IP must be local")
		}
	} else if host != "localhost" && !strings.HasSuffix(host, ".local") {
		return errors.New("candidate hostname must use local discovery")
	}
	if candidate.Kind == KindShelly && (candidate.Channel < 0 || candidate.Channel > 31) {
		return errors.New("shelly channel is outside supported bounds")
	}
	return nil
}

func (s *Service) probe(ctx context.Context, candidate Candidate) Result {
	result := Result{Kind: candidate.Kind, BaseURL: candidate.BaseURL, Channel: candidate.Channel}
	switch candidate.Kind {
	case KindShelly:
		status, err := s.shelly.GetSwitchStatus(ctx, candidate.BaseURL, candidate.Channel)
		if err != nil {
			result.Message = "Shelly did not respond to a read-only status request."
			return result
		}
		configuration, err := s.shelly.GetSwitchConfig(ctx, candidate.BaseURL, candidate.Channel)
		if err != nil {
			result.Message = "Shelly status responded but configuration could not be verified."
			return result
		}
		result.Reachable = true
		result.Identity = fmt.Sprintf("switch:%d", status.ID)
		result.Capabilities = []string{"switch", "reported-state", "power-telemetry"}
		result.Message = "Power-return policy: " + configuration.InitialState
	case KindESP32:
		snapshot, err := s.esp32.Snapshot(ctx, candidate.BaseURL, candidate.BearerToken)
		if err != nil {
			result.Message = "ESP32 node did not return a valid AquaOS snapshot."
			return result
		}
		result.Reachable = true
		result.Identity = snapshot.NodeID
		result.Firmware = snapshot.Firmware
		result.Capabilities = []string{"observe"}
		for _, probe := range snapshot.Probes {
			result.ProbeIDs = append(result.ProbeIDs, probe.SensorID)
		}
	default:
		result.Message = "Unsupported discovery kind."
	}
	return result
}
