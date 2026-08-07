// Package commissioning owns the explicit safety gate between discovering an
// endpoint and authorizing it for aquarium use.
package commissioning

import (
	"errors"
	"time"

	"github.com/tylerkirby004-droid/aquaos/internal/domain"
)

// Stage is the persisted progress of one physical endpoint.
type Stage string

//nolint:revive // Stage values are documented collectively by Stage.
const (
	StageDiscovered   Stage = "discovered"
	StageMapped       Stage = "mapped"
	StageConfigured   Stage = "configured"
	StageValidated    Stage = "validated"
	StageBenchTested  Stage = "bench-tested"
	StageCommissioned Stage = "commissioned"
	StageDisabled     Stage = "disabled"
)

// Evidence records operator-confirmed physical checks. AquaOS cannot infer
// wiring quality or independent safeguards from a network response.
type Evidence struct {
	SafeTestLoad                bool      `json:"safeTestLoad" yaml:"safe_test_load"`
	FailSafeStateVerified       bool      `json:"failSafeStateVerified" yaml:"fail_safe_state_verified"`
	PowerReturnVerified         bool      `json:"powerReturnVerified" yaml:"power_return_verified"`
	IndependentSafeguardPresent bool      `json:"independentSafeguardPresent" yaml:"independent_safeguard_present"`
	VerifiedBy                  string    `json:"verifiedBy" yaml:"verified_by"`
	VerifiedAt                  time.Time `json:"verifiedAt" yaml:"verified_at"`
}

// Record is the durable commissioning state for one physical endpoint.
type Record struct {
	EndpointID  domain.EndpointID  `json:"endpointId" yaml:"endpoint_id"`
	EquipmentID domain.EquipmentID `json:"equipmentId,omitempty" yaml:"equipment_id,omitempty"`
	Hazardous   bool               `json:"hazardous" yaml:"hazardous"`
	Stage       Stage              `json:"stage" yaml:"stage"`
	Evidence    Evidence           `json:"evidence" yaml:"evidence"`
}

// Advance validates a forward commissioning transition. Disabled endpoints
// can return only to configured state and must repeat all physical checks.
func (r Record) Advance(next Stage, evidence Evidence) (Record, error) {
	if err := r.EndpointID.Validate(); err != nil {
		return Record{}, err
	}
	allowed := map[Stage]Stage{
		StageDiscovered:  StageMapped,
		StageMapped:      StageConfigured,
		StageConfigured:  StageValidated,
		StageValidated:   StageBenchTested,
		StageBenchTested: StageCommissioned,
		StageDisabled:    StageConfigured,
	}
	if next == StageDisabled {
		r.Stage = next
		r.Evidence = Evidence{}
		return r, nil
	}
	if allowed[r.Stage] != next {
		return Record{}, errors.New("invalid commissioning stage transition")
	}
	if next == StageBenchTested {
		if !evidence.SafeTestLoad || !evidence.FailSafeStateVerified || !evidence.PowerReturnVerified || evidence.VerifiedBy == "" || evidence.VerifiedAt.IsZero() {
			return Record{}, errors.New("bench testing requires complete operator evidence")
		}
		if r.Hazardous && !evidence.IndependentSafeguardPresent {
			return Record{}, errors.New("hazardous equipment requires an independent physical safeguard")
		}
		r.Evidence = evidence
	}
	if next == StageCommissioned && (r.Evidence.VerifiedAt.IsZero() || (r.Hazardous && !r.Evidence.IndependentSafeguardPresent)) {
		return Record{}, errors.New("commissioning requires valid bench evidence")
	}
	r.Stage = next
	return r, nil
}

// Authorized reports whether normal control may target the endpoint.
func (r Record) Authorized() bool { return r.Stage == StageCommissioned }
