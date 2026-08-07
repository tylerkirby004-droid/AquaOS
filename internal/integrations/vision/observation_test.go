package vision

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func payload(confidence float64, expires time.Time) []byte {
	return []byte(fmt.Sprintf(`{"sourceAssetId":"11111111-1111-4111-8111-111111111111","serviceVersion":"0.1.0","modelVersion":"test","confidence":%g,"occurredAt":"2026-08-07T11:59:00Z","expiresAt":%q,"correlationId":"22222222-2222-4222-8222-222222222222","kind":"advisory","recommendation":"review"}`, confidence, expires.Format(time.RFC3339)))
}

func TestDecodeAcceptsCurrentHighConfidenceObservation(t *testing.T) {
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	if _, err := Decode(payload(.9, now.Add(time.Minute)), now); err != nil {
		t.Fatal(err)
	}
}

func TestDecodeRejectsStaleLowConfidenceMalformedAndOversized(t *testing.T) {
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	for _, candidate := range [][]byte{payload(.7, now.Add(time.Minute)), payload(.9, now.Add(-time.Second)), []byte("{"), []byte(strings.Repeat("x", maximumObservationBytes+1))} {
		if _, err := Decode(candidate, now); err == nil {
			t.Fatal("untrusted observation accepted")
		}
	}
}
