package bus

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"typefast/services/user/internal/revocation"
)

func newTestBus(t *testing.T) (*Redis, *miniredis.Miniredis) {
	t.Helper()

	server := miniredis.RunT(t)
	client, err := NewRedis("redis://"+server.Addr(), "test:revocations", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("new redis bus: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	// Keep reconnects instant so the tests stay fast.
	client.backoff = time.Millisecond
	return client, server
}

func TestPublishAndReceiveRoundTrip(t *testing.T) {
	client, _ := newTestBus(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	received := make(chan revocation.Event, 1)
	go client.Listen(ctx, func(event revocation.Event) { received <- event }, func() {})

	waitForSubscribers(t, client)

	event := revocation.Event{UserID: uuid.New(), Before: time.Now().UTC().Truncate(time.Second), Suspended: true}
	if err := client.Publish(ctx, event); err != nil {
		t.Fatalf("publish: %v", err)
	}

	select {
	case got := <-received:
		if got.UserID != event.UserID || !got.Suspended || !got.Before.Equal(event.Before) {
			t.Fatalf("received %+v, want %+v", got, event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("event was not delivered")
	}
}

func TestListenSkipsMalformedPayload(t *testing.T) {
	client, server := newTestBus(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	received := make(chan revocation.Event, 1)
	bus, err := NewRedis("redis://"+server.Addr(), "test:revocations", client.logger)
	if err != nil {
		t.Fatalf("new redis bus: %v", err)
	}
	defer bus.Close()
	bus.backoff = time.Millisecond
	go bus.Listen(ctx, func(event revocation.Event) { received <- event }, func() {})

	waitForSubscribers(t, bus)

	raw := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer raw.Close()
	if err := raw.Publish(ctx, "test:revocations", "not json").Err(); err != nil {
		t.Fatalf("publish raw: %v", err)
	}

	event := revocation.Event{UserID: uuid.New(), Before: time.Now().UTC()}
	if err := bus.Publish(ctx, event); err != nil {
		t.Fatalf("publish: %v", err)
	}

	select {
	case got := <-received:
		if got.UserID != event.UserID {
			t.Fatalf("received %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("listener stopped after a malformed message")
	}
}

func TestListenReportsGapWhenConnectionDrops(t *testing.T) {
	client, server := newTestBus(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	gaps := make(chan struct{}, 1)
	bus, err := NewRedis("redis://"+server.Addr(), "test:revocations", client.logger)
	if err != nil {
		t.Fatalf("new redis bus: %v", err)
	}
	defer bus.Close()
	bus.backoff = time.Millisecond
	go bus.Listen(ctx, func(revocation.Event) {}, func() {
		select {
		case gaps <- struct{}{}:
		default:
		}
	})

	waitForSubscribers(t, bus)
	server.Close()

	select {
	case <-gaps:
	case <-time.After(3 * time.Second):
		t.Fatal("a dropped connection must trigger a reload")
	}
}

func TestPublishFailsWhenServerIsDown(t *testing.T) {
	client, server := newTestBus(t)
	server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := client.Publish(ctx, revocation.Event{UserID: uuid.New()}); err == nil {
		t.Fatal("expected an error when Redis is unreachable")
	}
}

func TestNewRedisValidatesInput(t *testing.T) {
	if _, err := NewRedis("", "channel", nil); err == nil {
		t.Fatal("empty url must fail")
	}
	if _, err := NewRedis("redis://localhost:6379", "", nil); err == nil {
		t.Fatal("empty channel must fail")
	}
	if _, err := NewRedis("://broken", "channel", nil); err == nil {
		t.Fatal("an unparsable url must fail")
	}
}

type countingMetrics struct {
	mu      sync.Mutex
	results map[string]int
}

func (m *countingMetrics) AddRevocationEvent(result string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.results == nil {
		m.results = map[string]int{}
	}
	m.results[result]++
}

func (m *countingMetrics) count(result string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.results[result]
}

func TestBusRecordsOutcomes(t *testing.T) {
	client, _ := newTestBus(t)
	metrics := &countingMetrics{}
	client.SetMetrics(metrics)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	received := make(chan revocation.Event, 1)
	go client.Listen(ctx, func(event revocation.Event) { received <- event }, func() {})
	waitForSubscribers(t, client)

	if err := client.Publish(ctx, revocation.Event{UserID: uuid.New(), Before: time.Now().UTC()}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	select {
	case <-received:
	case <-time.After(2 * time.Second):
		t.Fatal("event was not delivered")
	}

	if metrics.count("published") != 1 {
		t.Fatalf("published = %d, want 1", metrics.count("published"))
	}
	if metrics.count("received") != 1 {
		t.Fatalf("received = %d, want 1", metrics.count("received"))
	}
}

// Two instances, one bus: a ban on one must be enforced by the other without
// waiting for its refresh timer.
func TestBanOnOneInstanceReachesAnother(t *testing.T) {
	_, server := newTestBus(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	speaker, err := NewRedis("redis://"+server.Addr(), "test:revocations", logger)
	if err != nil {
		t.Fatalf("speaker: %v", err)
	}
	defer speaker.Close()

	listener, err := NewRedis("redis://"+server.Addr(), "test:revocations", logger)
	if err != nil {
		t.Fatalf("listener: %v", err)
	}
	defer listener.Close()
	listener.backoff = time.Millisecond

	remote := revocation.NewRegistry(nil, logger)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go listener.Listen(ctx, remote.Apply, func() {})

	speakerRegistry := revocation.NewRegistry(nil, logger)
	speakerRegistry.SetPublisher(speaker)

	waitForSubscribers(t, listener)

	userID := uuid.New()
	banAt := time.Now().UTC()
	speakerRegistry.Revoke(userID, banAt)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if remote.TokenRevoked(userID, banAt.Add(time.Hour)) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the ban never reached the second instance")
}

// A lift has to travel the same way as the ban it reverses. If it only reached
// the instance that wrote it, every other instance would keep refusing fresh
// logins until its next refresh.
func TestLiftOnOneInstanceReachesAnother(t *testing.T) {
	_, server := newTestBus(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	speaker, err := NewRedis("redis://"+server.Addr(), "test:revocations", logger)
	if err != nil {
		t.Fatalf("speaker: %v", err)
	}
	defer speaker.Close()

	listener, err := NewRedis("redis://"+server.Addr(), "test:revocations", logger)
	if err != nil {
		t.Fatalf("listener: %v", err)
	}
	defer listener.Close()
	listener.backoff = time.Millisecond

	remote := revocation.NewRegistry(nil, logger)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go listener.Listen(ctx, remote.Apply, func() {})

	speakerRegistry := revocation.NewRegistry(nil, logger)
	speakerRegistry.SetPublisher(speaker)

	waitForSubscribers(t, listener)

	userID := uuid.New()
	banAt := time.Now().UTC()
	speakerRegistry.Revoke(userID, banAt)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !remote.TokenRevoked(userID, banAt.Add(time.Hour)) {
		time.Sleep(10 * time.Millisecond)
	}
	if !remote.TokenRevoked(userID, banAt.Add(time.Hour)) {
		t.Fatal("the ban never reached the second instance")
	}

	speakerRegistry.Restore(userID, banAt)

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && remote.TokenRevoked(userID, banAt.Add(time.Hour)) {
		time.Sleep(10 * time.Millisecond)
	}
	if remote.TokenRevoked(userID, banAt.Add(time.Hour)) {
		t.Fatal("the lift never reached the second instance")
	}
	if !remote.TokenRevoked(userID, banAt.Add(-time.Hour)) {
		t.Fatal("tokens issued before the ban must stay refused after the lift")
	}
}

// waitForSubscribers waits until at least one client is listening, so a
// publish is not sent before the subscription exists.
func waitForSubscribers(t *testing.T, client *Redis) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		counts, err := client.client.PubSubNumSub(context.Background(), client.channel).Result()
		if err == nil && counts[client.channel] > 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("subscriber never attached")
}
