package natsdiag

import (
	"context"
	"fmt"
	"strconv"
	"time"

	nc "github.com/nats-io/nats.go"
)

// Config contains only the connection settings needed for read-only
// JetStream inspection. It intentionally has no publish or subscribe options.
type Config struct {
	Host     string
	Port     int
	Username string
	Password string
	Token    string
}

type Inspector struct {
	config Config
}

type Snapshot struct {
	CollectedAt time.Time     `json:"collected_at"`
	RTT         time.Duration `json:"rtt" swaggertype:"integer"`
	Streams     []StreamInfo  `json:"streams"`
}

type StreamInfo struct {
	Name          string            `json:"name"`
	Subjects      []string          `json:"subjects"`
	SubjectCounts map[string]uint64 `json:"subject_counts,omitempty"`
	Messages      uint64            `json:"messages"`
	Bytes         uint64            `json:"bytes"`
	FirstSequence uint64            `json:"first_sequence"`
	LastSequence  uint64            `json:"last_sequence"`
	Consumers     []ConsumerInfo    `json:"consumers"`
}

type ConsumerInfo struct {
	Stream          string     `json:"stream"`
	Name            string     `json:"name"`
	FilterSubject   string     `json:"filter_subject,omitempty"`
	Pending         uint64     `json:"pending"`
	AckPending      int        `json:"ack_pending"`
	Redelivered     int        `json:"redelivered"`
	Waiting         int        `json:"waiting"`
	DeliveredStream uint64     `json:"delivered_stream_sequence"`
	AckFloorStream  uint64     `json:"ack_floor_stream_sequence"`
	LastActive      *time.Time `json:"last_active,omitempty"`
}

func New(config Config) (*Inspector, error) {
	if config.Host == "" {
		return nil, fmt.Errorf("nats host is required")
	}
	if config.Port < 1 || config.Port > 65535 {
		return nil, fmt.Errorf("nats port is invalid")
	}
	return &Inspector{config: config}, nil
}

func (i *Inspector) Snapshot(ctx context.Context) (Snapshot, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
	}
	started := time.Now()
	client, err := nc.Connect(i.config.connectionURL(), i.config.options()...)
	if err != nil {
		return Snapshot{}, fmt.Errorf("connect to nats: %w", err)
	}
	defer client.Close()
	if err := client.FlushWithContext(ctx); err != nil {
		return Snapshot{}, fmt.Errorf("ping nats: %w", err)
	}

	js, err := client.JetStream()
	if err != nil {
		return Snapshot{}, fmt.Errorf("create jetstream context: %w", err)
	}
	streams := make([]StreamInfo, 0)
	for info := range js.Streams() {
		if info == nil {
			continue
		}
		stream := StreamInfo{
			Name:          info.Config.Name,
			Subjects:      append([]string(nil), info.Config.Subjects...),
			SubjectCounts: info.State.Subjects,
			Messages:      info.State.Msgs,
			Bytes:         info.State.Bytes,
			FirstSequence: info.State.FirstSeq,
			LastSequence:  info.State.LastSeq,
			Consumers:     make([]ConsumerInfo, 0, info.State.Consumers),
		}
		for consumer := range js.Consumers(info.Config.Name) {
			if consumer == nil {
				continue
			}
			stream.Consumers = append(stream.Consumers, ConsumerInfo{
				Stream:          consumer.Stream,
				Name:            consumer.Name,
				FilterSubject:   consumer.Config.FilterSubject,
				Pending:         consumer.NumPending,
				AckPending:      consumer.NumAckPending,
				Redelivered:     consumer.NumRedelivered,
				Waiting:         consumer.NumWaiting,
				DeliveredStream: consumer.Delivered.Stream,
				AckFloorStream:  consumer.AckFloor.Stream,
				LastActive:      consumer.Delivered.Last,
			})
		}
		streams = append(streams, stream)
	}

	return Snapshot{CollectedAt: time.Now(), RTT: time.Since(started), Streams: streams}, nil
}

func (c Config) connectionURL() string {
	return "nats://" + c.Host + ":" + strconv.Itoa(c.Port)
}

func (c Config) options() []nc.Option {
	if c.Token != "" {
		return []nc.Option{nc.Token(c.Token)}
	}
	if c.Username != "" {
		return []nc.Option{nc.UserInfo(c.Username, c.Password)}
	}
	return nil
}
