package mqtt

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tylerkirby004-droid/aquaos/internal/domain"
)

// SchemaVersion is the current compatible message schema.
const SchemaVersion = "1.0"

var (
	// ErrOversizedPayload indicates configured resource limits were exceeded.
	ErrOversizedPayload = errors.New("MQTT payload exceeds configured maximum")
	// ErrUnknownVersion indicates an unsupported schema major version.
	ErrUnknownVersion = errors.New("unknown MQTT schema major version")
	// ErrExpiredMessage indicates a message is no longer actionable.
	ErrExpiredMessage = errors.New("MQTT message expired")
	// ErrDuplicateMessage indicates safe idempotent suppression.
	ErrDuplicateMessage = errors.New("duplicate MQTT message")
)

// Envelope is the versioned transport wrapper for public MQTT payloads.
type Envelope struct {
	SchemaVersion string               `json:"schemaVersion"`
	MessageID     string               `json:"messageId"`
	CorrelationID domain.CorrelationID `json:"correlationId"`
	Source        string               `json:"source"`
	SiteID        string               `json:"siteId"`
	OccurredAt    time.Time            `json:"occurredAt"`
	ExpiresAt     *time.Time           `json:"expiresAt,omitempty"`
	Revision      *domain.Revision     `json:"revision,omitempty"`
	Data          json.RawMessage      `json:"data"`
}

// CodecMetrics is a point-in-time copy of decode safety counters.
type CodecMetrics struct {
	Decoded        uint64 `json:"decoded"`
	Malformed      uint64 `json:"malformed"`
	Oversized      uint64 `json:"oversized"`
	Expired        uint64 `json:"expired"`
	UnknownVersion uint64 `json:"unknownVersion"`
	Duplicates     uint64 `json:"duplicates"`
}

// Codec applies strict schema, size, site, version, and expiry validation.
type Codec struct {
	siteID         string
	maximumPayload int
	now            func() time.Time
	decoded        atomic.Uint64
	malformed      atomic.Uint64
	oversized      atomic.Uint64
	expired        atomic.Uint64
	unknownVersion atomic.Uint64
	duplicates     atomic.Uint64
}

// NewCodec constructs a strict codec from external limits.
func NewCodec(siteID string, maximumPayload int, now func() time.Time) (*Codec, error) {
	if !topicSegmentPattern.MatchString(siteID) {
		return nil, errors.New("codec site ID must be lowercase kebab-case")
	}
	if maximumPayload < 256 || maximumPayload > 16*1024*1024 {
		return nil, errors.New("MQTT maximum payload must be between 256 bytes and 16 MiB")
	}
	if now == nil {
		return nil, errors.New("codec clock is required")
	}
	return &Codec{siteID: siteID, maximumPayload: maximumPayload, now: now}, nil
}

// Encode wraps and validates typed data without permitting arbitrary envelope fields.
func (c *Codec) Encode(messageID, source string, correlationID domain.CorrelationID, occurredAt time.Time, expiresAt *time.Time, revision *domain.Revision, data any) ([]byte, error) {
	encoded, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("encode MQTT data: %w", err)
	}
	envelope := Envelope{SchemaVersion: SchemaVersion, MessageID: messageID, CorrelationID: correlationID, Source: source, SiteID: c.siteID, OccurredAt: occurredAt.UTC(), ExpiresAt: cloneTime(expiresAt), Revision: cloneRevision(revision), Data: encoded}
	if err := c.validate(envelope); err != nil {
		return nil, err
	}
	payload, err := json.Marshal(envelope)
	if err != nil {
		return nil, err
	}
	if len(payload) > c.maximumPayload {
		return nil, ErrOversizedPayload
	}
	return payload, nil
}

// Decode rejects oversized, malformed, trailing, wrong-site, expired, and unknown-major messages.
func (c *Codec) Decode(payload []byte) (Envelope, error) {
	if len(payload) > c.maximumPayload {
		c.oversized.Add(1)
		return Envelope{}, ErrOversizedPayload
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var envelope Envelope
	if err := decoder.Decode(&envelope); err != nil {
		c.malformed.Add(1)
		return Envelope{}, fmt.Errorf("decode MQTT envelope: %w", err)
	}
	if err := ensureEOF(decoder); err != nil {
		c.malformed.Add(1)
		return Envelope{}, err
	}
	if major(envelope.SchemaVersion) != major(SchemaVersion) {
		c.unknownVersion.Add(1)
		return Envelope{}, ErrUnknownVersion
	}
	if err := c.validate(envelope); err != nil {
		if errors.Is(err, ErrExpiredMessage) {
			c.expired.Add(1)
		} else {
			c.malformed.Add(1)
		}
		return Envelope{}, err
	}
	c.decoded.Add(1)
	envelope.Data = append(json.RawMessage(nil), envelope.Data...)
	return envelope, nil
}

// Metrics returns lock-free decode counters.
func (c *Codec) Metrics() CodecMetrics {
	return CodecMetrics{Decoded: c.decoded.Load(), Malformed: c.malformed.Load(), Oversized: c.oversized.Load(), Expired: c.expired.Load(), UnknownVersion: c.unknownVersion.Load(), Duplicates: c.duplicates.Load()}
}

func (c *Codec) validate(envelope Envelope) error {
	if envelope.SchemaVersion == "" || envelope.MessageID == "" || len(envelope.MessageID) > 64 || envelope.Source == "" || envelope.SiteID != c.siteID || envelope.OccurredAt.IsZero() {
		return errors.New("MQTT envelope has invalid required fields")
	}
	if err := envelope.CorrelationID.Validate(); err != nil {
		return fmt.Errorf("MQTT correlation ID: %w", err)
	}
	if !json.Valid(envelope.Data) {
		return errors.New("MQTT envelope data must be valid JSON")
	}
	if envelope.ExpiresAt != nil && !c.now().UTC().Before(*envelope.ExpiresAt) {
		return ErrExpiredMessage
	}
	return nil
}
func ensureEOF(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("MQTT envelope contains trailing JSON")
	}
	return fmt.Errorf("decode trailing MQTT data: %w", err)
}
func major(version string) string { value, _, _ := strings.Cut(version, "."); return value }
func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := value.UTC()
	return &cloned
}
func cloneRevision(value *domain.Revision) *domain.Revision {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

// Consumer performs bounded idempotent decode before invoking business-facing routing.
type Consumer struct {
	mu       sync.Mutex
	codec    *Codec
	capacity int
	seen     map[string]struct{}
	order    []string
}

// NewConsumer constructs a bounded idempotency window.
func NewConsumer(codec *Codec, capacity int) (*Consumer, error) {
	if codec == nil {
		return nil, errors.New("MQTT codec is required")
	}
	if capacity < 1 || capacity > 1_000_000 {
		return nil, errors.New("MQTT idempotency capacity must be between 1 and 1000000")
	}
	return &Consumer{codec: codec, capacity: capacity, seen: make(map[string]struct{}, capacity), order: make([]string, 0, capacity)}, nil
}

// Consume validates and reserves a message ID before invoking handler. Handler
// failure removes the reservation so an explicit broker redelivery may retry.
func (c *Consumer) Consume(payload []byte, handler func(Envelope) error) error {
	if handler == nil {
		return errors.New("MQTT consumer handler is required")
	}
	envelope, err := c.codec.Decode(payload)
	if err != nil {
		return err
	}
	c.mu.Lock()
	if _, exists := c.seen[envelope.MessageID]; exists {
		c.mu.Unlock()
		c.codec.duplicates.Add(1)
		return ErrDuplicateMessage
	}
	c.seen[envelope.MessageID] = struct{}{}
	c.order = append(c.order, envelope.MessageID)
	if len(c.order) > c.capacity {
		oldest := c.order[0]
		c.order = c.order[1:]
		delete(c.seen, oldest)
	}
	c.mu.Unlock()
	if err := handler(envelope); err != nil {
		c.mu.Lock()
		delete(c.seen, envelope.MessageID)
		for index, id := range c.order {
			if id == envelope.MessageID {
				c.order = append(c.order[:index], c.order[index+1:]...)
				break
			}
		}
		c.mu.Unlock()
		return err
	}
	return nil
}
