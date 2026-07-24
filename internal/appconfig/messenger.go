package appconfig

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/ChiaYuChang/prism/internal/infra"
	prismlogger "github.com/ChiaYuChang/prism/pkg/logger"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

type MessengerConfig interface {
	NewMessenger(logger *slog.Logger, telemetry *infra.MessagingTelemetry) (*infra.Messenger, error)
}

type NatsConfig struct {
	Host             string        `mapstructure:"nats-host"         validate:"required"`
	Port             int           `mapstructure:"nats-port"         validate:"required,min=1,max=65535"`
	Username         string        `mapstructure:"nats-username"`
	Password         string        `mapstructure:"nats-password"`
	Token            string        `mapstructure:"nats-token"`
	QueueGroup       string        `mapstructure:"queue-group"       validate:"omitempty"`
	SubscribersCount int           `mapstructure:"subscribers-count" validate:"omitempty,min=1,max=64"`
	AckWaitTimeout   time.Duration `mapstructure:"ack-wait-timeout"  validate:"omitempty,min=1s"`
	Stream           string        `mapstructure:"nats-stream"`
	Consumer         string        `mapstructure:"nats-consumer"`
	AutoProvision    *bool         `mapstructure:"nats-auto-provision"`

	// File-based overrides for prod secret mounts. See PostgresConfig.PasswordFile.
	PasswordFile string `mapstructure:"nats-password-file"`
	TokenFile    string `mapstructure:"nats-token-file"`
}

// ResolveSecrets loads PasswordFile / TokenFile if set, overriding Password / Token.
func (n *NatsConfig) ResolveSecrets() error {
	if v, err := LoadFromFile(n.PasswordFile); err != nil {
		return err
	} else if v != "" {
		n.Password = v
	}
	if v, err := LoadFromFile(n.TokenFile); err != nil {
		return err
	} else if v != "" {
		n.Token = v
	}
	return nil
}

// String renders a human-readable summary with secrets redacted. The default
// fmt formatting paths (%v, %+v) call this, so logging a NatsConfig value will
// not leak the token or password.
func (n NatsConfig) String() string {
	return fmt.Sprintf(
		"host=%s port=%d username=%s password=%s token=%s queue_group=%s subscribers=%d ack_wait=%s stream=%s consumer=%s auto_provision=%t",
		n.Host, n.Port, n.Username,
		prismlogger.SecretMask(n.Password),
		prismlogger.SecretMask(n.Token),
		n.QueueGroup, n.SubscribersCount, n.AckWaitTimeout, n.Stream, n.Consumer, n.autoProvision(),
	)
}

// LogValue redacts secrets when the config is logged via slog.Any.
func (n NatsConfig) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("host", n.Host),
		slog.Int("port", n.Port),
		slog.String("username", n.Username),
		slog.String("password", prismlogger.SecretMask(n.Password)),
		slog.String("token", prismlogger.SecretMask(n.Token)),
		slog.String("queue_group", n.QueueGroup),
		slog.Int("subscribers", n.SubscribersCount),
		slog.Duration("ack_wait", n.AckWaitTimeout),
		slog.String("stream", n.Stream),
		slog.String("consumer", n.Consumer),
		slog.Bool("auto_provision", n.autoProvision()),
	)
}

func (n *NatsConfig) NewMessenger(logger *slog.Logger, telemetry *infra.MessagingTelemetry) (*infra.Messenger, error) {
	if (n.Stream == "") != (n.Consumer == "") {
		return nil, fmt.Errorf("nats stream and consumer must be configured together")
	}
	url := fmt.Sprintf("nats://%s:%d", n.Host, n.Port)

	if n.Token != "" {
		url = fmt.Sprintf("nats://%s@%s:%d", n.Token, n.Host, n.Port)
	} else if n.Username != "" {
		url = fmt.Sprintf("nats://%s:%s@%s:%d", n.Username, n.Password, n.Host, n.Port)
		if n.Password == "" {
			logger.Warn("connecting to NATS server without password")
		}
	} else {
		logger.Warn("connecting to NATS server without authentication")
	}

	opts := []infra.Option[infra.NatsConfig]{
		infra.WithQueueGroup(n.QueueGroup),
		infra.WithSubscribersCount(n.SubscribersCount),
		infra.WithAckWaitTimeout(n.AckWaitTimeout),
		infra.WithJetStreamAutoProvision(n.autoProvision()),
	}
	if n.Stream != "" {
		opts = append(opts, infra.WithConsumerBinding(n.Stream, n.Consumer))
	}
	if telemetry != nil {
		telemetryCopy := *telemetry
		if telemetryCopy.System == "" {
			telemetryCopy.System = "nats"
		}
		if telemetryCopy.Consumer == "" {
			telemetryCopy.Consumer = n.Consumer
		}
		if telemetryCopy.AckWaitTimeout <= 0 {
			telemetryCopy.AckWaitTimeout = n.AckWaitTimeout
		}
		telemetry = &telemetryCopy
	}
	return infra.NewNatsMessenger(
		url,
		logger,
		telemetry,
		opts...,
	)
}

func (n NatsConfig) autoProvision() bool {
	return n.AutoProvision == nil || *n.AutoProvision
}

type GoChannelConfig struct {
	ChannelBuffer int64 `mapstructure:"channel-buffer" validate:"omitempty,min=1"`
	Persistent    bool  `mapstructure:"persistent"`
}

func (g *GoChannelConfig) NewMessenger(logger *slog.Logger, telemetry *infra.MessagingTelemetry) (*infra.Messenger, error) {
	if telemetry != nil {
		telemetryCopy := *telemetry
		if telemetryCopy.System == "" {
			telemetryCopy.System = "gochannel"
		}
		telemetry = &telemetryCopy
	}
	return infra.NewGoChannelMessenger(
		logger,
		g.ChannelBuffer,
		g.Persistent,
		telemetry,
	)
}

// RegisterMessengerFlags registers standard CLI flags for messenger backends (nats and gochannel).
func RegisterMessengerFlags(fs *pflag.FlagSet, defaultQueueGroup string) {
	fs.String("messenger-type", "nats", "The messenger backend type (nats, gochannel)")
	fs.String("nats-host", "localhost", "The NATS server host")
	fs.Int("nats-port", 4222, "The NATS server port")
	fs.String("nats-token", "", "The NATS server auth token")
	fs.String("nats-token-file", "", "Path to file containing the NATS auth token")
	fs.String("queue-group", defaultQueueGroup, "Queue group for worker subscriptions")
	fs.Int("subscribers-count", 1, "How many subscriber goroutines to run")
	fs.Duration("ack-wait-timeout", 2*time.Minute, "Ack wait timeout for NATS subscriber")
	fs.Int64("channel-buffer", 100, "GoChannel output buffer size")
	fs.Bool("persistent", true, "Whether GoChannel should persist messages in memory")
}

// LoadMessengerConfig unmarshals and resolves the selected MessengerConfig based on messenger-type.
func LoadMessengerConfig(v *viper.Viper) (MessengerConfig, error) {
	messengerType := v.GetString("messenger-type")
	switch messengerType {
	case "nats":
		var natsConfig NatsConfig
		if err := v.Unmarshal(&natsConfig); err != nil {
			return nil, fmt.Errorf("failed to unmarshal NATS config: %w", err)
		}
		if err := natsConfig.ResolveSecrets(); err != nil {
			return nil, fmt.Errorf("resolve NATS secrets: %w", err)
		}
		if natsConfig.SubscribersCount == 0 {
			natsConfig.SubscribersCount = 1
		}
		if natsConfig.AckWaitTimeout == 0 {
			natsConfig.AckWaitTimeout = 2 * time.Minute
		}
		return &natsConfig, nil
	case "gochannel":
		var goChannelConfig GoChannelConfig
		if err := v.Unmarshal(&goChannelConfig); err != nil {
			return nil, fmt.Errorf("failed to unmarshal GoChannel config: %w", err)
		}
		if goChannelConfig.ChannelBuffer == 0 {
			goChannelConfig.ChannelBuffer = 100
		}
		return &goChannelConfig, nil
	default:
		return nil, fmt.Errorf("unsupported messenger type: %s", messengerType)
	}
}
