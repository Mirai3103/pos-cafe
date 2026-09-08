package eventbus_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"sync"
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

	err := bus.Subscribe(ctx, topic, func(ctx context.Context, payload []byte) error {
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
