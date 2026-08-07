package mqtt

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/tylerkirby004-droid/aquaos/internal/domain"
)

const testCorrelationID domain.CorrelationID = "10000000-0000-4000-8000-000000000001"

func TestCodecRejectsUnsafeMessages(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	codec, err := NewCodec("home-reef", 512, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	expiry := now.Add(time.Minute)
	valid, err := codec.Encode("message-1", "test", testCorrelationID, now, &expiry, nil, map[string]bool{"ok": true})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		payload []byte
		want    error
	}{{"oversized", make([]byte, 513), ErrOversizedPayload}, {"malformed", []byte(`{"schemaVersion":`), nil}, {"unknown version", replaceJSON(valid, `"1.0"`, `"2.0"`), ErrUnknownVersion}, {"wrong site", replaceJSON(valid, `"home-reef"`, `"other-site"`), nil}, {"unknown field", replaceJSON(valid, `"data":`, `"extra":true,"data":`), nil}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := codec.Decode(tt.payload)
			if tt.want != nil && !errors.Is(err, tt.want) {
				t.Fatalf("error=%v", err)
			}
			if tt.want == nil && err == nil {
				t.Fatal("unsafe message accepted")
			}
		})
	}
	now = expiry
	if _, err = codec.Decode(valid); !errors.Is(err, ErrExpiredMessage) {
		t.Fatalf("expired error=%v", err)
	}
	metrics := codec.Metrics()
	if metrics.Oversized != 1 || metrics.UnknownVersion != 1 || metrics.Expired != 1 || metrics.Malformed != 3 {
		t.Fatalf("unexpected codec metrics: %+v", metrics)
	}
}

func TestConsumerSuppressesDuplicatesAndBoundsMemory(t *testing.T) {
	now := time.Now().UTC()
	codec, _ := NewCodec("home-reef", 1024, func() time.Time { return now })
	consumer, _ := NewConsumer(codec, 1)
	calls := 0
	consume := func(id string) error {
		payload, err := codec.Encode(id, "test", testCorrelationID, now, nil, nil, map[string]bool{"ok": true})
		if err != nil {
			return err
		}
		return consumer.Consume(payload, func(Envelope) error { calls++; return nil })
	}
	if err := consume("one"); err != nil {
		t.Fatal(err)
	}
	if err := consume("one"); !errors.Is(err, ErrDuplicateMessage) {
		t.Fatalf("duplicate error=%v", err)
	}
	if err := consume("two"); err != nil {
		t.Fatal(err)
	}
	if err := consume("one"); err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatalf("calls=%d", calls)
	}
}

func replaceJSON(payload []byte, old, replacement string) []byte {
	return []byte(strings.Replace(string(payload), old, replacement, 1))
}
