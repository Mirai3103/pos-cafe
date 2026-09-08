package eventbus

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/pubsub/gochannel"
)

// HandlerFunc is the signature for event consumer handlers.
type HandlerFunc func(ctx context.Context, payload []byte) error

type Bus struct {
	pubSub *gochannel.GoChannel
	logger watermill.LoggerAdapter
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

// Subscribe listens on a topic and executes the handler function asynchronously for each message.
func (b *Bus) Subscribe(ctx context.Context, topic string, handler HandlerFunc) error {
	messages, err := b.pubSub.Subscribe(ctx, topic)
	if err != nil {
		return fmt.Errorf("subscribe to topic %s: %w", topic, err)
	}

	go func() {
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

// Close gracefully closes the event bus.
func (b *Bus) Close() error {
	return b.pubSub.Close()
}
