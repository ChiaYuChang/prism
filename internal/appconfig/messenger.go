package appconfig

import (
	"log/slog"
	"time"

	"github.com/ChiaYuChang/prism/internal/infra"
)

type MessengerConfig interface {
	NewMessenger(logger *slog.Logger) (*infra.Messenger, error)
}

type NatsConfig struct {
	URL              string        `mapstructure:"nats-url"          validate:"required,url"`
	QueueGroup       string        `mapstructure:"queue-group"       validate:"omitempty"`
	SubscribersCount int           `mapstructure:"subscribers-count" validate:"omitempty,min=1,max=64"`
	AckWaitTimeout   time.Duration `mapstructure:"ack-wait-timeout"  validate:"omitempty,min=1s"`
}

func (n *NatsConfig) NewMessenger(logger *slog.Logger) (*infra.Messenger, error) {
	return infra.NewNatsMessenger(
		n.URL,
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
