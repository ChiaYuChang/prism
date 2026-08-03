package natsadmin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

const allowTestPublishMetadata = "prism.allow_test_publish"

type Config struct {
	Host     string
	Port     int
	Username string
	Password string
	Token    string
}

type ApplyOptions struct {
	DryRun bool `json:"dry_run"`
}

type ApplyReport struct {
	Created   []string `json:"created"`
	Updated   []string `json:"updated"`
	Unchanged []string `json:"unchanged"`
}

type ConsumerRef struct {
	Stream   string `json:"stream"`
	Consumer string `json:"consumer"`
}

type PauseRequest struct {
	Until  time.Time `json:"until"`
	Reason string    `json:"reason,omitempty"`
}

type PauseResult struct {
	Stream      string     `json:"stream"`
	Consumer    string     `json:"consumer"`
	Paused      bool       `json:"paused"`
	PauseUntil  *time.Time `json:"pause_until,omitempty"`
	PauseReason string     `json:"reason,omitempty"`
}

type TestMessageResult struct {
	Stream   string `json:"stream"`
	Subject  string `json:"subject"`
	Sequence uint64 `json:"sequence"`
}

type Controller struct {
	config Config
}

func New(config Config) (*Controller, error) {
	if config.Host == "" {
		return nil, fmt.Errorf("host is required")
	}
	if config.Port < 1 || config.Port > 65535 {
		return nil, fmt.Errorf("port is invalid")
	}
	return &Controller{config: config}, nil
}

func (c *Controller) Apply(ctx context.Context, manifest Manifest, opts ApplyOptions) (ApplyReport, error) {
	if err := manifest.Validate(); err != nil {
		return ApplyReport{}, err
	}
	js, closeConn, err := c.connect()
	if err != nil {
		return ApplyReport{}, err
	}
	defer closeConn()

	var report ApplyReport
	createdStreams := make(map[string]struct{})
	for _, desired := range manifest.Streams {
		cfg, err := streamConfig(desired)
		if err != nil {
			return report, err
		}
		stream, getErr := js.Stream(ctx, desired.Name)
		if errors.Is(getErr, jetstream.ErrStreamNotFound) {
			report.Created = append(report.Created, "stream/"+desired.Name)
			createdStreams[desired.Name] = struct{}{}
			if !opts.DryRun {
				if _, err := js.CreateStream(ctx, cfg); err != nil {
					return report, fmt.Errorf("create stream %s: %w", desired.Name, err)
				}
			}
			continue
		}
		if getErr != nil {
			return report, fmt.Errorf("get stream %s: %w", desired.Name, getErr)
		}
		info, err := stream.Info(ctx)
		if err != nil {
			return report, fmt.Errorf("get stream %s info: %w", desired.Name, err)
		}
		if streamConfigEqual(info.Config, cfg) {
			report.Unchanged = append(report.Unchanged, "stream/"+desired.Name)
			continue
		}
		report.Updated = append(report.Updated, "stream/"+desired.Name)
		if !opts.DryRun {
			if _, err := js.UpdateStream(ctx, cfg); err != nil {
				return report, fmt.Errorf("update stream %s: %w", desired.Name, err)
			}
		}
	}

	for _, desired := range manifest.Consumers {
		cfg, err := consumerConfig(desired)
		if err != nil {
			return report, err
		}
		key := "consumer/" + desired.Stream + "/" + desired.Name
		if opts.DryRun {
			if _, streamWillBeCreated := createdStreams[desired.Stream]; streamWillBeCreated {
				report.Created = append(report.Created, key)
				continue
			}
		}
		consumer, getErr := js.PushConsumer(ctx, desired.Stream, desired.Name)
		if errors.Is(getErr, jetstream.ErrConsumerNotFound) {
			report.Created = append(report.Created, key)
			if !opts.DryRun {
				if _, err := js.CreatePushConsumer(ctx, desired.Stream, cfg); err != nil {
					return report, fmt.Errorf("create consumer %s: %w", key, err)
				}
			}
			continue
		}
		if getErr != nil {
			return report, fmt.Errorf("get consumer %s: %w", key, getErr)
		}
		info, err := consumer.Info(ctx)
		if err != nil {
			return report, fmt.Errorf("get consumer %s info: %w", key, err)
		}
		if consumerConfigEqual(info.Config, cfg) {
			report.Unchanged = append(report.Unchanged, key)
			continue
		}
		report.Updated = append(report.Updated, key)
		if !opts.DryRun {
			cfg.PauseUntil = info.Config.PauseUntil
			if _, err := js.UpdatePushConsumer(ctx, desired.Stream, cfg); err != nil {
				return report, fmt.Errorf("update consumer %s: %w", key, err)
			}
		}
	}
	return report, nil
}

func (c *Controller) PauseConsumer(ctx context.Context, ref ConsumerRef, req PauseRequest) (PauseResult, error) {
	if err := validateConsumerRef(ref); err != nil {
		return PauseResult{}, err
	}
	if req.Until.IsZero() || !req.Until.After(time.Now()) {
		return PauseResult{}, fmt.Errorf("pause until must be in the future")
	}
	js, closeConn, err := c.connect()
	if err != nil {
		return PauseResult{}, err
	}
	defer closeConn()
	if _, err := js.PauseConsumer(ctx, ref.Stream, ref.Consumer, req.Until); err != nil {
		return PauseResult{}, err
	}
	return PauseResult{Stream: ref.Stream, Consumer: ref.Consumer, Paused: true, PauseUntil: &req.Until, PauseReason: req.Reason}, nil
}

func (c *Controller) ResumeConsumer(ctx context.Context, ref ConsumerRef) (PauseResult, error) {
	if err := validateConsumerRef(ref); err != nil {
		return PauseResult{}, err
	}
	js, closeConn, err := c.connect()
	if err != nil {
		return PauseResult{}, err
	}
	defer closeConn()
	if _, err := js.ResumeConsumer(ctx, ref.Stream, ref.Consumer); err != nil {
		return PauseResult{}, err
	}
	return PauseResult{Stream: ref.Stream, Consumer: ref.Consumer}, nil
}

func (c *Controller) DeleteConsumer(ctx context.Context, ref ConsumerRef) error {
	if err := validateConsumerRef(ref); err != nil {
		return err
	}
	js, closeConn, err := c.connect()
	if err != nil {
		return err
	}
	defer closeConn()
	return js.DeleteConsumer(ctx, ref.Stream, ref.Consumer)
}

func (c *Controller) DeleteStream(ctx context.Context, stream string) error {
	if err := validateResourceName(stream); err != nil {
		return err
	}
	js, closeConn, err := c.connect()
	if err != nil {
		return err
	}
	defer closeConn()
	return js.DeleteStream(ctx, stream)
}

func (c *Controller) PurgeStream(ctx context.Context, streamName string) error {
	if err := validateResourceName(streamName); err != nil {
		return err
	}
	js, closeConn, err := c.connect()
	if err != nil {
		return err
	}
	defer closeConn()
	stream, err := js.Stream(ctx, streamName)
	if err != nil {
		return err
	}
	return stream.Purge(ctx)
}

func (c *Controller) PublishTestMessage(ctx context.Context, subject string) (TestMessageResult, error) {
	if subject != "prism_test" && !strings.HasPrefix(subject, "prism_test.") {
		return TestMessageResult{}, fmt.Errorf("test subject must use the prism_test namespace")
	}
	js, closeConn, err := c.connect()
	if err != nil {
		return TestMessageResult{}, err
	}
	defer closeConn()
	streamName, err := js.StreamNameBySubject(ctx, subject)
	if err != nil {
		return TestMessageResult{}, err
	}
	stream, err := js.Stream(ctx, streamName)
	if err != nil {
		return TestMessageResult{}, err
	}
	info, err := stream.Info(ctx)
	if err != nil {
		return TestMessageResult{}, err
	}
	if info.Config.Metadata[allowTestPublishMetadata] != "true" {
		return TestMessageResult{}, fmt.Errorf("stream %s does not allow test publishing", streamName)
	}
	payload, err := json.Marshal(map[string]any{"type": "prismctl.test", "sent_at": time.Now().UTC()})
	if err != nil {
		return TestMessageResult{}, err
	}
	ack, err := js.Publish(ctx, subject, payload)
	if err != nil {
		return TestMessageResult{}, err
	}
	return TestMessageResult{Stream: ack.Stream, Subject: subject, Sequence: ack.Sequence}, nil
}

func (c *Controller) connect() (jetstream.JetStream, func(), error) {
	serverURL := fmt.Sprintf("nats://%s:%d", c.config.Host, c.config.Port)
	options := make([]nats.Option, 0, 1)
	if c.config.Token != "" {
		options = append(options, nats.Token(c.config.Token))
	} else if c.config.Username != "" {
		options = append(options, nats.UserInfo(c.config.Username, c.config.Password))
	}
	conn, err := nats.Connect(serverURL, options...)
	if err != nil {
		return nil, nil, err
	}
	js, err := jetstream.New(conn)
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	return js, conn.Close, nil
}

func streamConfig(in StreamManifest) (jetstream.StreamConfig, error) {
	maxAge := time.Duration(0)
	var err error
	if in.MaxAge != "" {
		maxAge, err = time.ParseDuration(in.MaxAge)
		if err != nil {
			return jetstream.StreamConfig{}, err
		}
	}
	metadata := map[string]string{}
	if in.AllowTestPublish {
		metadata[allowTestPublishMetadata] = "true"
	}
	maxMessages := in.MaxMessages
	if maxMessages == 0 {
		maxMessages = -1
	}
	return jetstream.StreamConfig{
		Name: in.Name, Subjects: in.Subjects, Storage: storageType(in.Storage), Retention: retentionPolicy(in.Retention),
		Replicas: in.Replicas, MaxAge: maxAge, MaxMsgs: maxMessages, MaxBytes: -1, MaxConsumers: -1,
		MaxMsgsPerSubject: -1, Metadata: metadata,
	}, nil
}

func consumerConfig(in ConsumerManifest) (jetstream.ConsumerConfig, error) {
	ackWait, err := time.ParseDuration(in.AckWait)
	if err != nil {
		return jetstream.ConsumerConfig{}, err
	}
	return jetstream.ConsumerConfig{
		Name: in.Name, Durable: in.Name, DeliverSubject: "prism_delivery." + in.Name, DeliverGroup: in.QueueGroup,
		DeliverPolicy: jetstream.DeliverAllPolicy, AckPolicy: jetstream.AckExplicitPolicy, AckWait: ackWait,
		FilterSubject: in.FilterSubject, ReplayPolicy: jetstream.ReplayInstantPolicy,
		MaxAckPending: in.MaxAckPending, MaxDeliver: in.MaxDeliver,
	}, nil
}

func storageType(value string) jetstream.StorageType {
	if value == "memory" {
		return jetstream.MemoryStorage
	}
	return jetstream.FileStorage
}

func retentionPolicy(value string) jetstream.RetentionPolicy {
	switch value {
	case "interest":
		return jetstream.InterestPolicy
	case "workqueue":
		return jetstream.WorkQueuePolicy
	default:
		return jetstream.LimitsPolicy
	}
}

func streamConfigEqual(a, b jetstream.StreamConfig) bool {
	return slices.Equal(a.Subjects, b.Subjects) && a.Storage == b.Storage && a.Retention == b.Retention &&
		a.Replicas == b.Replicas && a.MaxAge == b.MaxAge && a.MaxMsgs == b.MaxMsgs &&
		a.Metadata[allowTestPublishMetadata] == b.Metadata[allowTestPublishMetadata]
}

func consumerConfigEqual(a, b jetstream.ConsumerConfig) bool {
	return a.Name == b.Name && a.Durable == b.Durable && a.DeliverSubject == b.DeliverSubject &&
		a.DeliverGroup == b.DeliverGroup && a.DeliverPolicy == b.DeliverPolicy && a.AckPolicy == b.AckPolicy &&
		a.AckWait == b.AckWait && a.FilterSubject == b.FilterSubject && a.MaxAckPending == b.MaxAckPending &&
		a.MaxDeliver == b.MaxDeliver
}

func validateResourceName(name string) error {
	if !resourceNamePattern.MatchString(name) {
		return fmt.Errorf("invalid resource name %q", name)
	}
	return nil
}

func validateConsumerRef(ref ConsumerRef) error {
	if err := validateResourceName(ref.Stream); err != nil {
		return err
	}
	return validateResourceName(ref.Consumer)
}
