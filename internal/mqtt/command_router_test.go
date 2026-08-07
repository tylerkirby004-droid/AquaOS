package mqtt

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tylerkirby004-droid/aquaos/internal/domain"
	"github.com/tylerkirby004-droid/aquaos/internal/output"
)

type recordingSubmitter struct{ calls int }

func (s *recordingSubmitter) Submit(_ context.Context, command output.Command) (output.Result, error) {
	s.calls++
	return output.Result{Command: command, Status: output.StatusAcknowledged, Reason: output.ReasonAcknowledged, UpdatedAt: command.IssuedAt}, nil
}

type recordingPublication struct{ values []Publication }

func (p *recordingPublication) PublishPublication(_ context.Context, value Publication) error {
	p.values = append(p.values, value)
	return nil
}

func TestCommandRouterIsIdempotentAndNeverRetainsResults(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	codec, _ := NewCodec("home-reef", 4096, func() time.Time { return now })
	consumer, _ := NewConsumer(codec, 8)
	topics, _ := NewRegistry("home-reef")
	submitter := &recordingSubmitter{}
	publisher := &recordingPublication{}
	router, err := NewCommandRouter(consumer, codec, topics, submitter, publisher, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	expires := now.Add(time.Minute)
	command := output.Command{ID: "20000000-0000-4000-8000-000000000002", IdempotencyKey: "external-1", CorrelationID: testCorrelationID, EquipmentID: "30000000-0000-4000-8000-000000000003", Requester: "home-assistant", IssuedAt: now, ExpiresAt: expires, On: true}
	payload, err := codec.Encode("request-1", "home-assistant", testCorrelationID, now, &expires, nil, command)
	if err != nil {
		t.Fatal(err)
	}
	if err = router.Handle(context.Background(), "heater-a", payload); err != nil {
		t.Fatal(err)
	}
	if err = router.Handle(context.Background(), "heater-a", payload); !errors.Is(err, ErrDuplicateMessage) {
		t.Fatalf("duplicate error=%v", err)
	}
	if submitter.calls != 1 || len(publisher.values) != 1 || publisher.values[0].Retained {
		t.Fatalf("calls=%d publications=%+v", submitter.calls, publisher.values)
	}
	if publisher.values[0].Topic != "aquaos/home-reef/v1/commands/heater-a/result" {
		t.Fatalf("topic=%s", publisher.values[0].Topic)
	}
}

var _ domain.CommandID = "00000000-0000-4000-8000-000000000000"
