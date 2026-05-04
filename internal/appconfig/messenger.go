package appconfig

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/ChiaYuChang/prism/internal/infra"
	prismlogger "github.com/ChiaYuChang/prism/pkg/logger"
)

type MessengerConfig interface {
	NewMessenger(logger *slog.Logger) (*infra.Messenger, error)
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
		"host=%s port=%d username=%s password=%s token=%s queue_group=%s subscribers=%d ack_wait=%s",
		n.Host, n.Port, n.Username,
		prismlogger.SecretMask(n.Password),
		prismlogger.SecretMask(n.Token),
		n.QueueGroup, n.SubscribersCount, n.AckWaitTimeout,
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
	)
}

func (n *NatsConfig) NewMessenger(logger *slog.Logger) (*infra.Messenger, error) {
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

	return infra.NewNatsMessenger(
		url,
		logger,
		infra.WithQueueGroup(n.QueueGroup),
		infra.WithSubscribersCount(n.SubscribersCount),
		infra.WithAckWaitTimeout(n.AckWaitTimeout),
	)
}

type GoChannelConfig struct {
	ChannelBuffer int64 `mapstructure:"channel-buffer" validate:"omitempty,min=1"`
	Persistent    bool  `mapstructure:"persistent"`
}

func (g *GoChannelConfig) NewMessenger(logger *slog.Logger) (*infra.Messenger, error) {
	return infra.NewGoChannelMessenger(
		logger,
		g.ChannelBuffer,
		g.Persistent,
	)
}
