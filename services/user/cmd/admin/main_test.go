package main

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"typefast/services/user/internal/identity"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestBusPublisher(t *testing.T) {
	t.Run("off by default", func(t *testing.T) {
		t.Setenv("REVOCATION_BUS", "")
		publisher, closeBus, err := busPublisher(testLogger())
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if publisher != nil {
			t.Fatal("publisher should be nil when the bus is off")
		}
		closeBus()
	})

	t.Run("none is off", func(t *testing.T) {
		t.Setenv("REVOCATION_BUS", "none")
		publisher, _, err := busPublisher(testLogger())
		if err != nil || publisher != nil {
			t.Fatalf("publisher = %v, err = %v, want nil, nil", publisher, err)
		}
	})

	t.Run("redis without URL is refused", func(t *testing.T) {
		t.Setenv("REVOCATION_BUS", "redis")
		t.Setenv("REDIS_URL", "")
		if _, _, err := busPublisher(testLogger()); err == nil {
			t.Fatal("expected an error when REDIS_URL is missing")
		}
	})

	t.Run("unknown mode is refused", func(t *testing.T) {
		t.Setenv("REVOCATION_BUS", "kafka")
		if _, _, err := busPublisher(testLogger()); err == nil {
			t.Fatal("expected an error for an unknown bus")
		}
	})

	t.Run("redis is connected lazily", func(t *testing.T) {
		t.Setenv("REVOCATION_BUS", "Redis")
		t.Setenv("REDIS_URL", "redis://127.0.0.1:6399/0")
		t.Setenv("REVOCATION_CHANNEL", "")
		publisher, closeBus, err := busPublisher(testLogger())
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if publisher == nil {
			t.Fatal("publisher should be set when the bus is redis")
		}
		closeBus()
	})
}

func TestMarkerFor(t *testing.T) {
	if got := markerFor(identity.User{}); !got.IsZero() {
		t.Fatalf("marker = %v, want zero for a user with no cut-off", got)
	}

	before := time.Date(2026, 11, 16, 10, 30, 0, 0, time.UTC)
	if got := markerFor(identity.User{RevokedBefore: &before}); !got.Equal(before) {
		t.Fatalf("marker = %v, want %v", got, before)
	}
}
