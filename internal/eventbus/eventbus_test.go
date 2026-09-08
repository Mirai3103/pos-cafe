package eventbus_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/eventbus"
)

type TestEvent struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

func TestEventBus_PublishSubscribe(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	bus := eventbus.New(logger)
	defer bus.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	const topic = "test.category.created"
	expectedEvent := TestEvent{
		ID:   42,
		Name: "Cà phê Robusta",
	}

	var wg sync.WaitGroup
	wg.Add(1)

	var receivedEvent TestEvent

	err := bus.Subscribe(ctx, topic, func(_ context.Context, payload []byte) error {
		defer wg.Done()
		return json.Unmarshal(payload, &receivedEvent)
	})
	if err != nil {
		t.Fatalf("failed to subscribe: %v", err)
	}

	// Give subscriber a tiny moment to register
	time.Sleep(10 * time.Millisecond)

	if err := bus.Publish(topic, expectedEvent); err != nil {
		t.Fatalf("failed to publish: %v", err)
	}

	wg.Wait()

	if receivedEvent.ID != expectedEvent.ID || receivedEvent.Name != expectedEvent.Name {
		t.Fatalf("expected event %+v, got %+v", expectedEvent, receivedEvent)
	}
}

// TestEventBus_CloseWaitsForInFlightHandlers pins the shutdown contract: Close
// must not return while a handler is still running, otherwise a process exiting
// right after Close can cut off side-effects mid-flight.
func TestEventBus_CloseWaitsForInFlightHandlers(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	bus := eventbus.New(logger)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	const topic = "test.slow.handler"

	var (
		started  = make(chan struct{})
		finished atomic.Bool
	)

	err := bus.Subscribe(ctx, topic, func(_ context.Context, _ []byte) error {
		close(started)
		// Simulate a side-effect that takes a moment (receipt print, audit write).
		time.Sleep(200 * time.Millisecond)
		finished.Store(true)
		return nil
	})
	if err != nil {
		t.Fatalf("failed to subscribe: %v", err)
	}

	if err := bus.Publish(topic, TestEvent{ID: 1, Name: "slow"}); err != nil {
		t.Fatalf("failed to publish: %v", err)
	}

	// Only close once the handler is provably mid-execution.
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("handler never started")
	}

	if err := bus.Close(); err != nil {
		t.Fatalf("close returned error: %v", err)
	}

	if !finished.Load() {
		t.Fatal("Close returned while a handler was still in flight")
	}
}
