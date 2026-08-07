package commissioning

import (
	"testing"
	"time"

	"github.com/tylerkirby004-droid/aquaos/internal/domain"
)

const endpointID domain.EndpointID = "10000000-0000-4000-8000-000000000001"

func TestHazardousCommissioningRequiresOrderedCompleteEvidence(t *testing.T) {
	record := Record{EndpointID: endpointID, Hazardous: true, Stage: StageDiscovered}
	for _, next := range []Stage{StageMapped, StageConfigured, StageValidated} {
		var err error
		record, err = record.Advance(next, Evidence{})
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := record.Advance(StageBenchTested, Evidence{SafeTestLoad: true}); err == nil {
		t.Fatal("incomplete bench evidence was accepted")
	}
	evidence := Evidence{SafeTestLoad: true, FailSafeStateVerified: true, PowerReturnVerified: true, IndependentSafeguardPresent: true, VerifiedBy: "operator", VerifiedAt: time.Now().UTC()}
	var err error
	record, err = record.Advance(StageBenchTested, evidence)
	if err != nil {
		t.Fatal(err)
	}
	record, err = record.Advance(StageCommissioned, Evidence{})
	if err != nil || !record.Authorized() {
		t.Fatalf("record=%+v err=%v", record, err)
	}
}

func TestDisabledEndpointLosesAuthorizationAndEvidence(t *testing.T) {
	record := Record{EndpointID: endpointID, Stage: StageCommissioned, Evidence: Evidence{VerifiedAt: time.Now()}}
	disabled, err := record.Advance(StageDisabled, Evidence{})
	if err != nil {
		t.Fatal(err)
	}
	if disabled.Authorized() || !disabled.Evidence.VerifiedAt.IsZero() {
		t.Fatalf("disabled record retained authority or evidence: %+v", disabled)
	}
}
