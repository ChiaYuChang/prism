package messaging

import (
	"context"
	"time"

	prismmessage "github.com/ChiaYuChang/prism/internal/message"
	wm "github.com/ThreeDotsLabs/watermill/message"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type publisher struct {
	next            wm.Publisher
	instrumentation *Instrumentation
}

func (p *publisher) Publish(topic string, messages ...*wm.Message) error {
	ctx := context.Background()
	if len(messages) > 0 && messages[0] != nil {
		ctx = messageContext(messages[0])
	}
	ctx, span := p.instrumentation.tracer.Start(
		ctx,
		"publish "+topic,
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(p.instrumentation.spanAttributes(topic, "publish")...),
	)
	defer span.End()

	started := time.Now()
	bytes := 0
	for _, msg := range messages {
		if err := prismmessage.InjectTraceContext(ctx, msg); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			p.instrumentation.metrics.recordPublish(ctx, p.instrumentation.system, topic, len(messages), bytes, started, err)
			return err
		}
		if msg != nil {
			msg.SetContext(ctx)
			bytes += len(msg.Payload)
		}
	}
	span.SetAttributes(
		attribute.Int("messaging.batch.message_count", len(messages)),
		attribute.Int("messaging.message.body.size", bytes),
	)

	err := p.next.Publish(topic, messages...)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	p.instrumentation.metrics.recordPublish(ctx, p.instrumentation.system, topic, len(messages), bytes, started, err)
	return err
}

func (p *publisher) Close() error {
	return p.next.Close()
}
