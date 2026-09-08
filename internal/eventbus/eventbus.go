package eventbus

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/pubsub/gochannel"
)

// HandlerFunc is the signature for event consumer handlers.
type HandlerFunc func(ctx context.Context, payload []byte) error

type Bus struct {
	pubSub *gochannel.GoChannel
	logger watermill.LoggerAdapter

	// wg tracks the consumer goroutines spawned by Subscribe so that Close
	// can wait for in-flight handlers to finish instead of abandoning them.
	wg sync.WaitGroup
}

// New creates a new In-Memory EventBus backed by Watermill GoChannel.
func New(logger *slog.Logger) *Bus {
	wmLogger := watermill.NewSlogLogger(logger)
	pubSub := gochannel.NewGoChannel(
		gochannel.Config{
			OutputChannelBuffer: 128,
			Persistent:          false,
		},
		wmLogger,
	)

	return &Bus{
		pubSub: pubSub,
		logger: wmLogger,
	}
}

// Publish serializes payload to JSON and publishes it to the specified topic.
func (b *Bus) Publish(topic string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal event payload for topic %s: %w", topic, err)
	}

	msg := message.NewMessage(watermill.NewUUID(), data)
	if err := b.pubSub.Publish(topic, msg); err != nil {
		return fmt.Errorf("publish event to topic %s: %w", topic, err)
	}

	return nil
}

// Subscribe listens on a topic and executes the handler asynchronously for each
// message.
//
// The consumer stops when either ctx is cancelled or Close is called. For
// subscriptions that must stay alive for the whole process lifetime, pass a
// context that is not tied to shutdown (see cmd/api/main.go) and let Close be
// the single stop signal — otherwise the consumer dies before the HTTP server
// has drained its in-flight requests, dropping the events those requests emit.
func (b *Bus) Subscribe(ctx context.Context, topic string, handler HandlerFunc) error {
	messages, err := b.pubSub.Subscribe(ctx, topic)
	if err != nil {
		return fmt.Errorf("subscribe to topic %s: %w", topic, err)
	}

	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-messages:
				if !ok {
					return
				}

				if err := handler(msg.Context(), msg.Payload); err != nil {
					b.logger.Error("event handler failed", err, watermill.LogFields{
						"topic":      topic,
						"message_id": msg.UUID,
					})
					msg.Nack()
				} else {
					msg.Ack()
				}
			}
		}
	}()

	return nil
}

// Close gracefully shuts the event bus down.
//
// Order matters: closing the underlying pub/sub closes every subscriber's
// message channel, which makes the consumer goroutines return. Only then can we
// wait on the WaitGroup — waiting first would deadlock, since nothing would have
// told the consumers to stop.
func (b *Bus) Close() error {
	err := b.pubSub.Close()
	b.wg.Wait()
	if err != nil {
		return fmt.Errorf("close event bus pubsub: %w", err)
	}
	return nil
}
