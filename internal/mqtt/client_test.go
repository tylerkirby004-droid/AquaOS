package mqtt

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
	"github.com/tylerkirby004-droid/aquaos/internal/config"
	"github.com/tylerkirby004-droid/aquaos/internal/domain"
)

type fakeToken struct{ err error }

func (t fakeToken) WaitTimeout(time.Duration) bool { return true }
func (t fakeToken) Error() error                   { return t.err }

type fakeBroker struct {
	mu                              sync.Mutex
	connected                       bool
	connects, subscribes, publishes int
	published                       []Publication
}

func (b *fakeBroker) Connect() token {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.connects++
	b.connected = true
	return fakeToken{}
}
func (b *fakeBroker) Disconnect(uint)   { b.mu.Lock(); b.connected = false; b.mu.Unlock() }
func (b *fakeBroker) IsConnected() bool { b.mu.Lock(); defer b.mu.Unlock(); return b.connected }
func (b *fakeBroker) Publish(topic string, qos byte, retained bool, payload interface{}) token {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.publishes++
	value, _ := payload.([]byte)
	b.published = append(b.published, Publication{Topic: topic, QoS: qos, Retained: retained, Payload: append([]byte(nil), value...)})
	return fakeToken{}
}
func (b *fakeBroker) Subscribe(string, byte, paho.MessageHandler) token {
	b.mu.Lock()
	b.subscribes++
	b.mu.Unlock()
	return fakeToken{}
}
func (b *fakeBroker) setDisconnected() { b.mu.Lock(); b.connected = false; b.mu.Unlock() }
func (b *fakeBroker) counts() (int, int, int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.connects, b.subscribes, b.publishes
}

func mqttConfig() config.MQTT {
	return config.MQTT{Enabled: true, SiteID: "home-reef", Broker: "tcp://localhost:1883", ClientID: "test", ConnectTimeout: 100 * time.Millisecond, KeepAlive: time.Second, DisconnectQuiesce: time.Millisecond, MaximumPayload: 4096, QueueCapacity: 2, IdempotencyCapacity: 8, ReconnectMinimum: time.Millisecond, ReconnectMaximum: 10 * time.Millisecond, ReconnectJitter: 0}
}
func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }
func fakeClient(t *testing.T, broker *fakeBroker) *Client {
	t.Helper()
	cfg := mqttConfig()
	registry, _ := NewRegistry(cfg.SiteID)
	codec, _ := NewCodec(cfg.SiteID, cfg.MaximumPayload, time.Now)
	return newClient(cfg, discardLogger(), registry, codec, broker, rand.New(rand.NewSource(1)))
}

func TestNewClientStartsDisconnected(t *testing.T) {
	client, err := New(mqttConfig(), discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	if client.Health().Healthy {
		t.Fatal("disconnected client is healthy")
	}
}

func TestReconnectResubscribesReconcilesAndPublishesBirth(t *testing.T) {
	broker := &fakeBroker{}
	client := fakeClient(t, broker)
	var reconciles atomic.Int32
	client.SetReconciler(func(context.Context) error { reconciles.Add(1); return nil })
	if err := client.Subscribe(context.Background(), "aquaos/home-reef/v1/commands/+/request", 1, func(paho.Client, paho.Message) {}); err != nil {
		t.Fatal(err)
	}
	if err := client.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	broker.setDisconnected()
	client.signalReconnect()
	deadline := time.Now().Add(time.Second)
	for {
		connects, subscribes, publishes := broker.counts()
		if connects >= 2 && subscribes >= 2 && publishes >= 2 && reconciles.Load() >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("connects=%d subscribes=%d publishes=%d reconciles=%d", connects, subscribes, publishes, reconciles.Load())
		}
		time.Sleep(time.Millisecond)
	}
	if err := client.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestOfflineQueueIsBoundedAndDropsExpiredOnReconnect(t *testing.T) {
	broker := &fakeBroker{}
	client := fakeClient(t, broker)
	expired := time.Now().Add(-time.Second)
	future := time.Now().Add(time.Minute)
	if err := client.PublishPublication(context.Background(), Publication{Topic: "aquaos/home-reef/v1/events/test", QoS: 1, Payload: []byte(`{}`), ExpiresAt: &expired}); !errors.Is(err, ErrExpiredMessage) {
		t.Fatalf("expired error=%v", err)
	}
	for index := 0; index < 2; index++ {
		if err := client.PublishPublication(context.Background(), Publication{Topic: "aquaos/home-reef/v1/events/test", QoS: 1, Payload: []byte(`{}`), ExpiresAt: &future}); err != nil {
			t.Fatal(err)
		}
	}
	if err := client.PublishPublication(context.Background(), Publication{Topic: "aquaos/home-reef/v1/events/test", QoS: 1, Payload: []byte(`{}`), ExpiresAt: &future}); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("queue error=%v", err)
	}
	if err := client.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, _, publishes := broker.counts()
	if publishes != 3 {
		t.Fatalf("publishes=%d want birth plus two queued", publishes)
	}
	_ = client.Stop(context.Background())
}

func TestPublicationRejectsRetainedCommandsAndEvents(t *testing.T) {
	for _, topic := range []string{"aquaos/home-reef/v1/commands/heater/request", "aquaos/home-reef/v1/events/alarm"} {
		if err := validatePublication(topic, 1, true); err == nil {
			t.Fatalf("retained topic accepted: %s", topic)
		}
	}
}

func TestSubscriptionRejectsBroadWildcards(t *testing.T) {
	client := fakeClient(t, &fakeBroker{})
	handler := func(paho.Client, paho.Message) {}
	for _, topic := range []string{"#", "aquaos/#", "aquaos/+/v1/commands/+/request"} {
		if err := client.Subscribe(context.Background(), topic, 1, handler); err == nil {
			t.Fatalf("broad subscription accepted: %s", topic)
		}
	}
	for _, topic := range []string{"aquaos/home-reef/v1/commands/+/request", "aquaos/home-reef/v1/ai/+/observations/+"} {
		if err := client.Subscribe(context.Background(), topic, 1, handler); err != nil {
			t.Fatalf("contract subscription rejected: %s: %v", topic, err)
		}
	}
}

func TestLogsRedactCredentialsAndPayloads(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	broker := &fakeBroker{}
	client := fakeClient(t, broker)
	client.logger = logger
	client.cfg.Username = "sensitive-user"
	client.cfg.Password = "sensitive-password"
	if err := client.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	secretPayload := []byte(`{"token":"sensitive-payload"}`)
	if err := client.Publish(context.Background(), "aquaos/home-reef/v1/events/test", 1, false, secretPayload); err != nil {
		t.Fatal(err)
	}
	if err := client.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"sensitive-user", "sensitive-password", "sensitive-payload"} {
		if bytes.Contains(logs.Bytes(), []byte(secret)) {
			t.Fatalf("secret appeared in logs: %s", secret)
		}
	}
}

func TestAvailabilityCorrelationFailureIsExplicit(t *testing.T) {
	client := fakeClient(t, &fakeBroker{})
	client.newCorrelation = func() (domain.CorrelationID, error) { return "", errors.New("entropy unavailable") }
	if err := client.Start(context.Background()); err == nil {
		t.Fatal("availability correlation failure was ignored")
	}
}
