package shared

import (
	"github.com/ThreeDotsLabs/watermill/message"
)

// Messenger is a wrapper around Watermill's message components
type Messenger struct {
	Publisher  message.Publisher
	Subscriber message.Subscriber
	Router     *message.Router
}
