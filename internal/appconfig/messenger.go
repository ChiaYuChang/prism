package appconfig

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/ChiaYuChang/prism/internal/infra"
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
