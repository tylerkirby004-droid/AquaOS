// Package mqtt implements AquaOS's optional external integration transport.
// It never contains equipment or safety policy.
package mqtt

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
	"github.com/tylerkirby004-droid/aquaos/internal/config"
	"github.com/tylerkirby004-droid/aquaos/internal/domain"
	"github.com/tylerkirby004-droid/aquaos/internal/health"
)

// MQTTClient is the broker transport boundary; it contains no domain policy.
//
//nolint:revive // The required public contract intentionally names the transport.
type MQTTClient interface {
	health.Component
	Publish(context.Context, string, byte, bool, []byte) error
	Subscribe(context.Context, string, byte, paho.MessageHandler) error
}

var (
	// ErrDisconnected indicates that immediate delivery is unavailable.
	ErrDisconnected = errors.New("MQTT broker is disconnected")
	// ErrQueueFull indicates bounded offline capacity was exhausted.
	ErrQueueFull = errors.New("MQTT offline queue is full")
)

// Publication is one bounded offline-capable outbound message.
type Publication struct {
	Topic     string
	QoS       byte
	Retained  bool
	Payload   []byte
	ExpiresAt *time.Time
}

// TransportMetrics is a point-in-time copy of operational counters.
type TransportMetrics struct {
	Connects       uint64 `json:"connects"`
	Reconnects     uint64 `json:"reconnects"`
	Published      uint64 `json:"published"`
	Received       uint64 `json:"received"`
	DecodeErrors   uint64 `json:"decodeErrors"`
	DroppedExpired uint64 `json:"droppedExpired"`
	QueueRejected  uint64 `json:"queueRejected"`
}

type token interface {
	WaitTimeout(time.Duration) bool
	Error() error
}
type brokerClient interface {
	Connect() token
	Disconnect(uint)
	IsConnected() bool
	Publish(string, byte, bool, interface{}) token
	Subscribe(string, byte, paho.MessageHandler) token
}
type pahoClient struct{ paho.Client }

func (p pahoClient) Connect() token { return p.Client.Connect() }
func (p pahoClient) Publish(topic string, qos byte, retained bool, payload interface{}) token {
	return p.Client.Publish(topic, qos, retained, payload)
}
func (p pahoClient) Subscribe(topic string, qos byte, handler paho.MessageHandler) token {
	return p.Client.Subscribe(topic, qos, handler)
}

type subscription struct {
	topic   string
	qos     byte
	handler paho.MessageHandler
}

// Client owns connection, resubscription, reconciliation, and bounded offline delivery.
type Client struct {
	client         brokerClient
	cfg            config.MQTT
	logger         *slog.Logger
	registry       *Registry
	codec          *Codec
	mu             sync.RWMutex
	lastErr        error
	subscriptions  map[string]subscription
	queue          []Publication
	reconcile      func(context.Context) error
	cancel         context.CancelFunc
	reconnect      chan struct{}
	done           chan struct{}
	random         *rand.Rand
	now            func() time.Time
	newCorrelation func() (domain.CorrelationID, error)
	connects       atomic.Uint64
	reconnects     atomic.Uint64
	published      atomic.Uint64
	received       atomic.Uint64
	droppedExpired atomic.Uint64
	queueRejected  atomic.Uint64
}

// New constructs an Eclipse Paho client with retained offline LWT.
func New(cfg config.MQTT, logger *slog.Logger) (*Client, error) {
	if logger == nil {
		return nil, errors.New("MQTT logger is required")
	}
	registry, err := NewRegistry(cfg.SiteID)
	if err != nil {
		return nil, err
	}
	codec, err := NewCodec(cfg.SiteID, cfg.MaximumPayload, time.Now)
	if err != nil {
		return nil, err
	}
	client := newClient(cfg, logger, registry, codec, nil, rand.New(rand.NewSource(time.Now().UnixNano())))
	availability, policy, _ := registry.Topic(PurposeAvailability, "core")
	lwt, err := client.availabilityPayload("offline")
	if err != nil {
		return nil, err
	}
	options := paho.NewClientOptions().AddBroker(cfg.Broker).SetClientID(cfg.ClientID).SetUsername(cfg.Username).SetPassword(cfg.Password).SetConnectTimeout(cfg.ConnectTimeout).SetKeepAlive(cfg.KeepAlive).SetAutoReconnect(false).SetCleanSession(false).SetOrderMatters(true).SetWill(availability, string(lwt), policy.QoS, policy.Retained).SetConnectionLostHandler(func(_ paho.Client, connectionErr error) {
		client.setError(connectionErr)
		client.signalReconnect()
		logger.Error("MQTT connection lost", "code", "mqtt.connection_lost", "error", connectionErr)
	})
	client.client = pahoClient{Client: paho.NewClient(options)}
	return client, nil
}

func newClient(cfg config.MQTT, logger *slog.Logger, registry *Registry, codec *Codec, broker brokerClient, random *rand.Rand) *Client {
	return &Client{client: broker, cfg: cfg, logger: logger, registry: registry, codec: codec, subscriptions: make(map[string]subscription), queue: make([]Publication, 0, cfg.QueueCapacity), reconnect: make(chan struct{}, 1), random: random, now: time.Now, newCorrelation: domain.NewCorrelationID}
}

// Name returns the lifecycle component name.
func (c *Client) Name() string { return "mqtt" }

// Start establishes the initial connection, restores subscriptions, reconciles
// retained state, publishes birth, then starts the owned reconnect loop.
func (c *Client) Start(ctx context.Context) error {
	if c.client == nil {
		return errors.New("MQTT broker client is required")
	}
	if err := c.connect(ctx, false); err != nil {
		return err
	}
	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	c.mu.Lock()
	c.cancel = cancel
	c.done = done
	c.mu.Unlock()
	go func() { defer close(done); c.reconnectLoop(runCtx) }()
	return nil
}

// Stop publishes graceful offline availability, cancels reconnect work, waits,
// then disconnects within the caller's shutdown context.
func (c *Client) Stop(ctx context.Context) error {
	c.mu.Lock()
	cancel := c.cancel
	done := c.done
	c.cancel = nil
	c.done = nil
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if c.client.IsConnected() {
		_ = c.publishAvailability(ctx, "offline")
		c.client.Disconnect(uint(c.cfg.DisconnectQuiesce.Milliseconds()))
	}
	return nil
}

// Health reports external integration connectivity without affecting core control readiness policy.
func (c *Client) Health() health.Status {
	c.mu.RLock()
	lastErr := c.lastErr
	c.mu.RUnlock()
	connected := c.client != nil && c.client.IsConnected()
	condition := health.StateUnhealthy
	if connected {
		condition = health.StateHealthy
	}
	status := health.NewStatus(c.Name(), condition, "", time.Now().UTC())
	if lastErr != nil {
		status.Message = lastErr.Error()
	} else if !connected {
		status.Message = "disconnected"
	}
	return status
}

// SetReconciler installs the transport-neutral retained-state reconciliation hook.
func (c *Client) SetReconciler(reconcile func(context.Context) error) {
	c.mu.Lock()
	c.reconcile = reconcile
	c.mu.Unlock()
}

// Publish performs immediate opaque delivery and never queues implicitly.
func (c *Client) Publish(ctx context.Context, topic string, qos byte, retained bool, payload []byte) error {
	if err := validatePublication(topic, qos, retained); err != nil {
		return err
	}
	if !c.client.IsConnected() {
		return ErrDisconnected
	}
	if err := waitToken(ctx, c.client.Publish(topic, qos, retained, append([]byte(nil), payload...)), c.cfg.ConnectTimeout); err != nil {
		return err
	}
	c.published.Add(1)
	return nil
}

// PublishPublication publishes immediately or enqueues a bounded, expiring copy.
func (c *Client) PublishPublication(ctx context.Context, publication Publication) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validatePublication(publication.Topic, publication.QoS, publication.Retained); err != nil {
		return err
	}
	if len(publication.Payload) > c.cfg.MaximumPayload {
		return ErrOversizedPayload
	}
	if publication.ExpiresAt != nil && !time.Now().UTC().Before(*publication.ExpiresAt) {
		c.droppedExpired.Add(1)
		return ErrExpiredMessage
	}
	if c.client.IsConnected() {
		return c.Publish(ctx, publication.Topic, publication.QoS, publication.Retained, publication.Payload)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.queue) >= c.cfg.QueueCapacity {
		c.queueRejected.Add(1)
		return ErrQueueFull
	}
	publication.Payload = append([]byte(nil), publication.Payload...)
	publication.ExpiresAt = cloneTime(publication.ExpiresAt)
	c.queue = append(c.queue, publication)
	return nil
}

// Subscribe records a narrow subscription and applies it immediately when connected.
func (c *Client) Subscribe(ctx context.Context, topic string, qos byte, handler paho.MessageHandler) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !c.validSubscription(topic) || qos > 1 || handler == nil {
		return errors.New("invalid MQTT subscription")
	}
	wrapped := func(client paho.Client, message paho.Message) { c.received.Add(1); handler(client, message) }
	c.mu.Lock()
	c.subscriptions[topic] = subscription{topic: topic, qos: qos, handler: wrapped}
	c.mu.Unlock()
	if !c.client.IsConnected() {
		return nil
	}
	return waitToken(ctx, c.client.Subscribe(topic, qos, wrapped), c.cfg.ConnectTimeout)
}

// Metrics returns lock-free transport counters.
func (c *Client) Metrics() TransportMetrics {
	codecMetrics := c.codec.Metrics()
	decodeErrors := codecMetrics.Malformed + codecMetrics.Oversized + codecMetrics.UnknownVersion + codecMetrics.Expired
	return TransportMetrics{Connects: c.connects.Load(), Reconnects: c.reconnects.Load(), Published: c.published.Load(), Received: c.received.Load(), DecodeErrors: decodeErrors, DroppedExpired: c.droppedExpired.Load(), QueueRejected: c.queueRejected.Load()}
}

func (c *Client) connect(ctx context.Context, reconnect bool) error {
	if err := waitToken(ctx, c.client.Connect(), c.cfg.ConnectTimeout); err != nil {
		c.setError(err)
		return fmt.Errorf("connect MQTT: %w", err)
	}
	c.connects.Add(1)
	if reconnect {
		c.reconnects.Add(1)
	}
	c.setError(nil)
	if err := c.restore(ctx); err != nil {
		return err
	}
	c.logger.InfoContext(ctx, "MQTT connected", "code", "mqtt.connected", "site_id", c.cfg.SiteID, "reconnect", reconnect)
	return nil
}
func (c *Client) restore(ctx context.Context) error {
	c.mu.RLock()
	subscriptions := make([]subscription, 0, len(c.subscriptions))
	for _, item := range c.subscriptions {
		subscriptions = append(subscriptions, item)
	}
	reconcile := c.reconcile
	c.mu.RUnlock()
	for _, item := range subscriptions {
		if err := waitToken(ctx, c.client.Subscribe(item.topic, item.qos, item.handler), c.cfg.ConnectTimeout); err != nil {
			return fmt.Errorf("restore MQTT subscription: %w", err)
		}
	}
	if reconcile != nil {
		if err := reconcile(ctx); err != nil {
			return fmt.Errorf("reconcile retained MQTT state: %w", err)
		}
	}
	if err := c.publishAvailability(ctx, "online"); err != nil {
		return err
	}
	return c.flush(ctx)
}
func (c *Client) publishAvailability(ctx context.Context, status string) error {
	topic, policy, err := c.registry.Topic(PurposeAvailability, "core")
	if err != nil {
		return err
	}
	payload, err := c.availabilityPayload(status)
	if err != nil {
		return err
	}
	return c.Publish(ctx, topic, policy.QoS, policy.Retained, payload)
}
func (c *Client) availabilityPayload(status string) ([]byte, error) {
	correlationID, err := c.newCorrelation()
	if err != nil {
		return nil, fmt.Errorf("create MQTT availability correlation ID: %w", err)
	}
	now := c.now().UTC()
	return c.codec.Encode("availability-"+fmt.Sprint(now.UnixNano()), "aquaos-core", correlationID, now, nil, nil, map[string]string{"status": status})
}
func (c *Client) flush(ctx context.Context) error {
	for {
		c.mu.Lock()
		if len(c.queue) == 0 {
			c.mu.Unlock()
			return nil
		}
		publication := c.queue[0]
		c.queue = c.queue[1:]
		c.mu.Unlock()
		if publication.ExpiresAt != nil && !time.Now().UTC().Before(*publication.ExpiresAt) {
			c.droppedExpired.Add(1)
			continue
		}
		if err := c.Publish(ctx, publication.Topic, publication.QoS, publication.Retained, publication.Payload); err != nil {
			c.mu.Lock()
			c.queue = append([]Publication{publication}, c.queue...)
			c.mu.Unlock()
			return err
		}
	}
}
func (c *Client) reconnectLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.reconnect:
		}
		delay := c.cfg.ReconnectMinimum
		for {
			timer := time.NewTimer(c.jitter(delay))
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			attemptCtx, cancel := context.WithTimeout(ctx, c.cfg.ConnectTimeout)
			err := c.connect(attemptCtx, true)
			cancel()
			if err == nil {
				break
			}
			c.logger.Warn("MQTT reconnect failed", "code", "mqtt.reconnect_failed", "error", err)
			delay = time.Duration(math.Min(float64(c.cfg.ReconnectMaximum), float64(delay)*2))
		}
	}
}
func (c *Client) jitter(delay time.Duration) time.Duration {
	factor := 1 + (c.random.Float64()*2-1)*c.cfg.ReconnectJitter
	result := time.Duration(float64(delay) * factor)
	if result < time.Millisecond {
		return time.Millisecond
	}
	return result
}
func (c *Client) signalReconnect() {
	select {
	case c.reconnect <- struct{}{}:
	default:
	}
}
func (c *Client) setError(err error) { c.mu.Lock(); c.lastErr = err; c.mu.Unlock() }
func (c *Client) validSubscription(topic string) bool {
	if topic == "" || containsSegment(topic, "#") || topic == "#" {
		return false
	}
	if !containsWildcard(topic) {
		return true
	}
	commandFilter, _, commandErr := c.registry.SubscriptionFilter(PurposeCommandRequest)
	aiFilter, _, aiErr := c.registry.SubscriptionFilter(PurposeAIObservation)
	return (commandErr == nil && topic == commandFilter) || (aiErr == nil && topic == aiFilter)
}
func validatePublication(topic string, qos byte, retained bool) error {
	if topic == "" || containsWildcard(topic) {
		return errors.New("invalid MQTT publication topic")
	}
	if qos > 1 {
		return errors.New("unsupported MQTT QoS")
	}
	if retained && (containsSegment(topic, "commands") || containsSegment(topic, "events")) {
		return errors.New("commands and events must never be retained")
	}
	return nil
}
func containsWildcard(value string) bool {
	for _, character := range value {
		if character == '#' || character == '+' {
			return true
		}
	}
	return false
}
func containsSegment(topic, segment string) bool {
	for _, part := range splitTopic(topic) {
		if part == segment {
			return true
		}
	}
	return false
}
func splitTopic(topic string) []string {
	var parts []string
	start := 0
	for index, character := range topic {
		if character == '/' {
			parts = append(parts, topic[start:index])
			start = index + 1
		}
	}
	return append(parts, topic[start:])
}
func waitToken(ctx context.Context, value token, timeout time.Duration) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		if value.WaitTimeout(0) {
			return value.Error()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return context.DeadlineExceeded
		case <-ticker.C:
		}
	}
}
