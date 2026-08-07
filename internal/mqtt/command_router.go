package mqtt

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/tylerkirby004-droid/aquaos/internal/output"
)

// CommandSubmitter is the consumer-owned core command-policy boundary.
type CommandSubmitter interface {
	Submit(context.Context, output.Command) (output.Result, error)
}

// PublicationPublisher is the bounded MQTT publication boundary used by routing.
type PublicationPublisher interface {
	PublishPublication(context.Context, Publication) error
}

// CommandRouter decodes external requests and delegates all policy to output.Service.
type CommandRouter struct {
	consumer  *Consumer
	codec     *Codec
	topics    *Registry
	commands  CommandSubmitter
	publisher PublicationPublisher
	now       func() time.Time
}

// NewCommandRouter constructs a transport-only command router.
func NewCommandRouter(consumer *Consumer, codec *Codec, topics *Registry, commands CommandSubmitter, publisher PublicationPublisher, now func() time.Time) (*CommandRouter, error) {
	if consumer == nil || codec == nil || topics == nil || commands == nil || publisher == nil || now == nil {
		return nil, errors.New("all MQTT command router dependencies are required")
	}
	return &CommandRouter{consumer: consumer, codec: codec, topics: topics, commands: commands, publisher: publisher, now: now}, nil
}

// Handle validates one request envelope, submits it through Core safety policy,
// and publishes a non-retained result. MQTT never executes equipment logic.
func (r *CommandRouter) Handle(ctx context.Context, target string, payload []byte) error {
	return r.consumer.Consume(payload, func(envelope Envelope) error {
		var command output.Command
		decoder := json.NewDecoder(bytes.NewReader(envelope.Data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&command); err != nil {
			return fmt.Errorf("decode MQTT command request: %w", err)
		}
		if err := ensureEOF(decoder); err != nil {
			return fmt.Errorf("decode trailing MQTT command data: %w", err)
		}
		if command.CorrelationID != envelope.CorrelationID {
			return errors.New("command and envelope correlation IDs differ")
		}
		if envelope.ExpiresAt == nil || command.ExpiresAt.After(*envelope.ExpiresAt) {
			return errors.New("command expiry exceeds envelope expiry")
		}
		result, submitErr := r.commands.Submit(ctx, command)
		if submitErr != nil {
			return submitErr
		}
		topic, policy, err := r.topics.Topic(PurposeCommandResult, target)
		if err != nil {
			return err
		}
		response, err := r.codec.Encode("result-"+string(command.ID), "aquaos-core", envelope.CorrelationID, r.now().UTC(), envelope.ExpiresAt, command.ExpectedRevision, result)
		if err != nil {
			return err
		}
		if policy.Retained {
			return errors.New("command result policy must not retain")
		}
		return r.publisher.PublishPublication(ctx, Publication{Topic: topic, QoS: policy.QoS, Retained: false, Payload: response, ExpiresAt: envelope.ExpiresAt})
	})
}
