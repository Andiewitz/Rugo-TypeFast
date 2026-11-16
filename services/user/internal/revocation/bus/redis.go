// Package bus carries revocation changes between API instances over Redis.
//
// Redis is a messenger here, never a source of truth. A message that is lost
// only delays a ban until the registry refreshes from PostgreSQL, and nothing
// in a request ever waits on this package.
package bus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"

	"typefast/services/user/internal/revocation"
)

// Metrics counts what the bus does, so a broker that has gone quiet is
// visible instead of only being felt as slow ban propagation.
type Metrics interface {
	AddRevocationEvent(result string)
}

// Redis publishes and receives revocation events on one channel.
type Redis struct {
	client  *redis.Client
	channel string
	logger  *slog.Logger
	metrics Metrics
	backoff time.Duration
}

// NewRedis prepares a connection to Redis. The connection is lazy, so an
// unreachable server never stops the API from starting.
func NewRedis(url, channel string, logger *slog.Logger) (*Redis, error) {
	if url == "" {
		return nil, errors.New("bus: redis url is empty")
	}
	if channel == "" {
		return nil, errors.New("bus: channel is empty")
	}
	if logger == nil {
		logger = slog.Default()
	}

	options, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("bus: parse redis url: %w", err)
	}

	return &Redis{
		client:  redis.NewClient(options),
		channel: channel,
		logger:  logger,
		backoff: time.Second,
	}, nil
}

// SetMetrics records bus outcomes. A nil value records nothing.
func (r *Redis) SetMetrics(metrics Metrics) {
	r.metrics = metrics
}

// Close releases the connection.
func (r *Redis) Close() error {
	return r.client.Close()
}

// Publish broadcasts one event. The caller decides what a failure means; the
// registry logs it and carries on, because storage already holds the change.
func (r *Redis) Publish(ctx context.Context, event revocation.Event) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("bus: encode event: %w", err)
	}

	if err := r.client.Publish(ctx, r.channel, payload).Err(); err != nil {
		r.record("failed")
		return fmt.Errorf("bus: publish: %w", err)
	}
	r.record("published")
	return nil
}

// Listen applies every event it receives until ctx is cancelled.
//
// A subscribe or receive failure means messages may have been missed, so it
// calls onGap — which reloads the full set from PostgreSQL — waits, and
// subscribes again. A message that will not decode is logged and skipped; one
// bad payload must not stop the listener.
func (r *Redis) Listen(ctx context.Context, onEvent func(revocation.Event), onGap func()) {
	for {
		if ctx.Err() != nil {
			return
		}

		subscription := r.client.Subscribe(ctx, r.channel)

		for {
			message, err := subscription.ReceiveMessage(ctx)
			if err != nil {
				_ = subscription.Close()
				if ctx.Err() != nil {
					return
				}
				if !r.wait(ctx, "receive", err, onGap) {
					return
				}
				break
			}

			var event revocation.Event
			if err := json.Unmarshal([]byte(message.Payload), &event); err != nil {
				r.logger.Warn("revocation event ignored", "error", err)
				r.record("malformed")
				continue
			}
			r.record("received")
			onEvent(event)
		}
	}
}

// wait logs the failure, reports the gap, and sleeps before reconnecting. It
// returns false when the context ended while waiting.
func (r *Redis) record(result string) {
	if r.metrics != nil {
		r.metrics.AddRevocationEvent(result)
	}
}

func (r *Redis) wait(ctx context.Context, action string, err error, onGap func()) bool {
	r.record("gap")
	r.logger.Warn("revocation bus interrupted", "action", action, "error", err)
	onGap()

	timer := time.NewTimer(r.backoff)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
