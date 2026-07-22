package messaging

import (
	"context"
	"sync"
	"time"

	wm "github.com/ThreeDotsLabs/watermill/message"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type subscriber struct {
	next            wm.Subscriber
	instrumentation *Instrumentation
}

func (s *subscriber) Subscribe(ctx context.Context, topic string) (<-chan *wm.Message, error) {
	messages, err := s.next.Subscribe(ctx, topic)
	if err != nil {
		s.instrumentation.metrics.recordSubscribeError(ctx, s.instrumentation.system, topic, s.instrumentation.consumer)
		return nil, err
	}

	out := make(chan *wm.Message)
	monitorCtx, cancel := context.WithCancel(ctx)
	go func() {
		defer close(out)
		defer cancel()
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-messages:
				if !ok {
					return
				}
				if msg == nil {
					continue
				}

				msgCtx, span := s.instrumentation.tracer.Start(
					messageContext(msg),
					"process "+topic,
					trace.WithSpanKind(trace.SpanKindConsumer),
					trace.WithAttributes(s.instrumentation.spanAttributes(topic, "process")...),
				)
				msg.SetContext(msgCtx)
				span.SetAttributes(attribute.Int("messaging.message.body.size", len(msg.Payload)))
				started := time.Now()
				s.instrumentation.metrics.inflight.Add(
					msgCtx,
					1,
					s.instrumentation.metrics.attributes(s.instrumentation.system, topic, "process", "", s.instrumentation.consumer),
				)

				var once sync.Once
				finish := func(outcome string, outcomeErr error) {
					once.Do(func() {
						if outcomeErr != nil {
							span.RecordError(outcomeErr)
							span.SetStatus(codes.Error, outcomeErr.Error())
						}
						span.SetAttributes(attribute.String("outcome", outcome))
						span.End()
						attrs := s.instrumentation.metrics.attributes(s.instrumentation.system, topic, "process", outcome, s.instrumentation.consumer)
						s.instrumentation.metrics.processMessages.Add(msgCtx, 1, attrs)
						s.instrumentation.metrics.processDuration.Record(msgCtx, time.Since(started).Seconds(), attrs)
						s.instrumentation.metrics.processBytes.Record(msgCtx, int64(len(msg.Payload)), attrs)
						s.instrumentation.metrics.inflight.Add(msgCtx, -1, attrs)
						if outcomeErr != nil {
							s.instrumentation.metrics.processErrors.Add(msgCtx, 1, attrs)
						}
					})
				}

				go waitForOutcome(monitorCtx, msg, s.instrumentation.ackWaitTimeout, finish)
				select {
				case out <- msg:
				case <-monitorCtx.Done():
					finish("shutdown", monitorCtx.Err())
					return
				}
			}
		}
	}()
	return out, nil
}

func (s *subscriber) Close() error {
	return s.next.Close()
}

func waitForOutcome(ctx context.Context, msg *wm.Message, timeout time.Duration, finish func(string, error)) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-msg.Acked():
		finish("ack", nil)
	case <-msg.Nacked():
		finish("nack", context.Canceled)
	case <-timer.C:
		finish("ack_timeout", context.DeadlineExceeded)
	case <-ctx.Done():
		finish("shutdown", ctx.Err())
	}
}

var _ wm.Publisher = (*publisher)(nil)
var _ wm.Subscriber = (*subscriber)(nil)
