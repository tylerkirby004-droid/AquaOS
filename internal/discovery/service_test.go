package discovery

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tylerkirby004-droid/aquaos/internal/adapters/esp32"
	"github.com/tylerkirby004-droid/aquaos/internal/adapters/shelly"
)

type fakeShelly struct{ err error }

func (f fakeShelly) GetSwitchStatus(context.Context, string, int) (shelly.SwitchStatus, error) {
	return shelly.SwitchStatus{ID: 0}, f.err
}
func (f fakeShelly) GetSwitchConfig(context.Context, string, int) (shelly.SwitchConfig, error) {
	return shelly.SwitchConfig{ID: 0, InitialState: "off"}, f.err
}

type fakeESP32 struct{ err error }

func (f fakeESP32) Snapshot(context.Context, string, string) (esp32.SnapshotDTO, error) {
	return esp32.SnapshotDTO{NodeID: "node-1", Firmware: "1.0", Probes: []esp32.ProbeDTO{{SensorID: "probe-a"}, {SensorID: "probe-b"}}}, f.err
}

func TestProbeReturnsTypedReadOnlyResults(t *testing.T) {
	service, err := New(fakeShelly{}, fakeESP32{}, 2, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	results, err := service.Probe(context.Background(), []Candidate{{Kind: KindESP32, BaseURL: "http://node.local"}, {Kind: KindShelly, BaseURL: "http://plug.local", Channel: 0}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || !results[0].Reachable || !results[1].Reachable || len(results[0].ProbeIDs) != 2 {
		t.Fatalf("results = %+v", results)
	}
}

func TestProbeBoundsAndRedactsTransportErrors(t *testing.T) {
	service, _ := New(fakeShelly{err: errors.New("credential=secret")}, fakeESP32{}, 1, time.Second)
	results, err := service.Probe(context.Background(), []Candidate{{Kind: KindShelly, BaseURL: "http://plug.local"}})
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Reachable || results[0].Message == "credential=secret" {
		t.Fatalf("unsafe discovery result = %+v", results[0])
	}
	tooMany := make([]Candidate, maximumCandidates+1)
	if _, err = service.Probe(context.Background(), tooMany); err == nil {
		t.Fatal("unbounded candidate set was accepted")
	}
}

func TestESP32CredentialFileIsRestrictedAndResolved(t *testing.T) {
	service, err := New(fakeShelly{}, fakeESP32{}, 1, time.Second, func(path string) (string, error) {
		if path != "/etc/aquaos/secrets/node.token" {
			t.Fatalf("secret path = %q", path)
		}
		return "token", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	results, err := service.Probe(context.Background(), []Candidate{{Kind: KindESP32, BaseURL: "http://node.local", BearerTokenFile: "/etc/aquaos/secrets/node.token"}})
	if err != nil || !results[0].Reachable {
		t.Fatalf("results=%+v err=%v", results, err)
	}
	if _, err = service.Probe(context.Background(), []Candidate{{Kind: KindESP32, BaseURL: "http://node.local", BearerTokenFile: "/etc/passwd"}}); err == nil {
		t.Fatal("credential file outside AquaOS secrets was accepted")
	}
}
