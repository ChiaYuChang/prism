package infra

import (
	"fmt"
	"log/slog"
	"time"

	messaginginstrumentation "github.com/ChiaYuChang/prism/internal/infra/messaging"
	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill-nats/v2/pkg/nats"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/pubsub/gochannel"
	nc "github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// Option is a generic functional option type.
type Option[T any] func(*T)

// Messenger provides a unified entry point for message publishing and subscribing.
type Messenger struct {
	message.Publisher
	message.Subscriber
}

// MessagingTelemetry configures OpenTelemetry instrumentation for a messenger.
type MessagingTelemetry struct {
	Tracer         trace.Tracer
	Meter          metric.Meter
	System         string
	Consumer       string
	AckWaitTimeout time.Duration
}

// NatsConfig combines publisher and subscriber configurations for NATS JetStream.
type NatsConfig struct {
	Pub nats.PublisherConfig
	Sub nats.SubscriberConfig
}

// WithQueueGroup sets the queue group for the subscriber.
func WithQueueGroup(group string) Option[NatsConfig] {
	return func(c *NatsConfig) {
		c.Sub.QueueGroupPrefix = group
	}
}

// WithSubscribersCount sets how many concurrent goroutines should consume messages.
func WithSubscribersCount(n int) Option[NatsConfig] {
	return func(c *NatsConfig) {
		c.Sub.SubscribersCount = n
	}
}

// WithAckWaitTimeout sets how long the subscriber should wait for an Ack/Nack.
func WithAckWaitTimeout(d time.Duration) Option[NatsConfig] {
	return func(c *NatsConfig) {
		c.Sub.AckWaitTimeout = d
		c.Sub.JetStream.SubscribeOptions = append(c.Sub.JetStream.SubscribeOptions, nc.AckWait(d))
	}
}

func WithJetStreamAutoProvision(enabled bool) Option[NatsConfig] {
	return func(c *NatsConfig) {
		c.Pub.JetStream.AutoProvision = enabled
		c.Sub.JetStream.AutoProvision = enabled
	}
}

func WithConsumerBinding(stream, consumer string) Option[NatsConfig] {
	return func(c *NatsConfig) {
		c.Sub.JetStream.AutoProvision = false
		c.Sub.JetStream.DurablePrefix = consumer
		c.Sub.JetStream.SubscribeOptions = append(c.Sub.JetStream.SubscribeOptions, nc.Bind(stream, consumer))
	}
}

// WithPubNatsOptions allows passing custom nats.Option specifically for the publisher.
func WithPubNatsOptions(opts ...nc.Option) Option[NatsConfig] {
	return func(c *NatsConfig) {
		c.Pub.NatsOptions = append(c.Pub.NatsOptions, opts...)
	}
}

// WithSubNatsOptions allows passing custom nats.Option specifically for the subscriber.
func WithSubNatsOptions(opts ...nc.Option) Option[NatsConfig] {
	return func(c *NatsConfig) {
		c.Sub.NatsOptions = append(c.Sub.NatsOptions, opts...)
	}
}

// WithNatsOptions allows passing custom nats.Option for both publisher and subscriber.
func WithNatsOptions(opts ...nc.Option) Option[NatsConfig] {
	return func(c *NatsConfig) {
		c.Pub.NatsOptions = append(c.Pub.NatsOptions, opts...)
		c.Sub.NatsOptions = append(c.Sub.NatsOptions, opts...)
	}
}

// NewNatsMessenger creates a Messenger using NATS JetStream with generic optional configurations.
func NewNatsMessenger(url string, logger *slog.Logger, telemetry *MessagingTelemetry, opts ...Option[NatsConfig]) (*Messenger, error) {
	watermillLogger := watermill.NewSlogLogger(logger)

	// Default configurations
	cfg := &NatsConfig{
		Pub: nats.PublisherConfig{
			URL:       url,
			Marshaler: nats.JSONMarshaler{},
			JetStream: nats.JetStreamConfig{
				Disabled:      false,
				AutoProvision: true,
			},
		},
		Sub: nats.SubscriberConfig{
			URL:              url,
			Unmarshaler:      nats.JSONMarshaler{},
			SubscribersCount: 1,
			CloseTimeout:     time.Minute,
			AckWaitTimeout:   time.Second * 30,
			JetStream: nats.JetStreamConfig{
				Disabled:      false,
				AutoProvision: true,
			},
		},
	}

	// Apply generic functional options
	for _, opt := range opts {
		opt(cfg)
	}

	// 1. Setup JetStream Publisher
	publisher, err := nats.NewPublisher(cfg.Pub, watermillLogger)
	if err != nil {
		return nil, fmt.Errorf("failed to create NATS publisher: %w", err)
	}

	// 2. Setup JetStream Subscriber
	subscriber, err := nats.NewSubscriber(cfg.Sub, watermillLogger)
	if err != nil {
		return nil, fmt.Errorf("failed to create NATS subscriber: %w", err)
	}

	messenger := &Messenger{
		Publisher:  publisher,
		Subscriber: subscriber,
	}
	return instrumentMessenger(messenger, telemetry)
}

// NewGoChannelMessenger creates a Messenger using in-memory Go Channels.
// This is ideal for testing and local development without NATS.
func NewGoChannelMessenger(logger *slog.Logger, buffer int64, persistent bool, telemetry *MessagingTelemetry) (*Messenger, error) {
	watermillLogger := watermill.NewSlogLogger(logger)

	// Use GoChannel as both Publisher and Subscriber
	pubSub := gochannel.NewGoChannel(
		gochannel.Config{
			OutputChannelBuffer:            buffer,
			Persistent:                     persistent,
			BlockPublishUntilSubscriberAck: true,
		},
		watermillLogger,
	)

	messenger := &Messenger{
		Publisher:  pubSub,
		Subscriber: pubSub,
	}
	return instrumentMessenger(messenger, telemetry)
}

func instrumentMessenger(messenger *Messenger, telemetry *MessagingTelemetry) (*Messenger, error) {
	if telemetry == nil {
		return messenger, nil
	}
	instrumentation, err := messaginginstrumentation.New(messaginginstrumentation.Config{
		Tracer:         telemetry.Tracer,
		Meter:          telemetry.Meter,
		System:         telemetry.System,
		Consumer:       telemetry.Consumer,
		AckWaitTimeout: telemetry.AckWaitTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("initialize messenger instrumentation: %w", err)
	}
	publisher, err := instrumentation.Publisher(messenger.Publisher)
	if err != nil {
		return nil, fmt.Errorf("instrument messenger publisher: %w", err)
	}
	subscriber, err := instrumentation.Subscriber(messenger.Subscriber)
	if err != nil {
		return nil, fmt.Errorf("instrument messenger subscriber: %w", err)
	}
	return &Messenger{Publisher: publisher, Subscriber: subscriber}, nil
}

// Close gracefully shuts down both the publisher and subscriber.
func (m *Messenger) Close() error {
	if err := m.Publisher.Close(); err != nil {
		return err
	}
	return m.Subscriber.Close()
}
