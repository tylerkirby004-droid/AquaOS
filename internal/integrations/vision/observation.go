// Package vision validates untrusted, optional AI observations.
package vision

import (
	"bytes"
	"encoding/json"
	"errors"
	"time"

	"github.com/tylerkirby004-droid/aquaos/internal/domain"
)

const maximumObservationBytes = 16 * 1024

// Observation is the version-one advisory AI contract.
type Observation struct {
	SourceAssetID  domain.DeviceID      `json:"sourceAssetId"`
	ServiceVersion string               `json:"serviceVersion"`
	ModelVersion   string               `json:"modelVersion"`
	Confidence     float64              `json:"confidence"`
	OccurredAt     time.Time            `json:"occurredAt"`
	ExpiresAt      time.Time            `json:"expiresAt"`
	CorrelationID  domain.CorrelationID `json:"correlationId"`
	Kind           string               `json:"kind"`
	Recommendation string               `json:"recommendation"`
}

// Decode validates one bounded observation at the trust boundary.
func Decode(payload []byte, now time.Time) (Observation, error) {
	if len(payload) == 0 || len(payload) > maximumObservationBytes {
		return Observation{}, errors.New("vision observation has invalid size")
	}
	var result Observation
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return Observation{}, errors.New("vision observation is malformed")
	}
	if result.SourceAssetID.Validate() != nil || result.CorrelationID.Validate() != nil || result.ServiceVersion == "" || result.ModelVersion == "" || result.Kind == "" {
		return Observation{}, errors.New("vision observation identity is invalid")
	}
	if result.Confidence < 0.8 || result.Confidence > 1 || result.OccurredAt.After(now) || !result.ExpiresAt.After(now) || !result.ExpiresAt.After(result.OccurredAt) {
		return Observation{}, errors.New("vision observation is stale or below confidence policy")
	}
	if decoder.Decode(&struct{}{}) == nil {
		return Observation{}, errors.New("vision observation contains multiple values")
	}
	return result, nil
}
