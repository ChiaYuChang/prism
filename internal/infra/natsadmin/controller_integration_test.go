package natsadmin_test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/ChiaYuChang/prism/internal/appconfig"
	"github.com/ChiaYuChang/prism/internal/infra/natsadmin"
	wm "github.com/ThreeDotsLabs/watermill/message"
	"github.com/stretchr/testify/require"
)

func TestControllerLifecycleIntegration(t *testing.T) {
	if os.Getenv("PRISM_NATS_INTEGRATION") != "1" {
		t.Skip("set PRISM_NATS_INTEGRATION=1 to run against local NATS")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	suffix := time.Now().UnixNano()
	stream := fmt.Sprintf("prism_test_%d", suffix)
	subject := fmt.Sprintf("prism_test.%d", suffix)
	consumer := fmt.Sprintf("test-worker-%d", suffix)

	controller, err := natsadmin.New(natsadmin.Config{Host: "localhost", Port: 4222, Token: os.Getenv("NATS_AUTH_TOKEN")})
	require.NoError(t, err)
	manifest := natsadmin.Manifest{
		Version: 1,
		Streams: []natsadmin.StreamManifest{{
			Name: stream, Subjects: []string{subject}, Storage: "memory", Retention: "limits", Replicas: 1, AllowTestPublish: true,
		}},
		Consumers: []natsadmin.ConsumerManifest{{
			Stream: stream, Name: consumer, FilterSubject: subject, QueueGroup: consumer,
			DeliverPolicy: "all", AckPolicy: "explicit", AckWait: "30s", MaxAckPending: 1, MaxDeliver: 3,
		}},
	}
	t.Cleanup(func() { _ = controller.DeleteStream(context.Background(), stream) })

	report, err := controller.Apply(ctx, manifest, natsadmin.ApplyOptions{DryRun: true})
	require.NoError(t, err)
	require.Len(t, report.Created, 2)
	report, err = controller.Apply(ctx, manifest, natsadmin.ApplyOptions{})
	require.NoError(t, err)
	require.Len(t, report.Created, 2)
	report, err = controller.Apply(ctx, manifest, natsadmin.ApplyOptions{})
	require.NoError(t, err)
	require.Len(t, report.Unchanged, 2)

	disabled := false
	messenger, err := (&appconfig.NatsConfig{
		Host: "localhost", Port: 4222, Token: os.Getenv("NATS_AUTH_TOKEN"), QueueGroup: consumer,
		SubscribersCount: 1, AckWaitTimeout: 30 * time.Second, Stream: stream, Consumer: consumer, AutoProvision: &disabled,
	}).NewMessenger(slog.Default())
	require.NoError(t, err)
	messages, err := messenger.Subscribe(ctx, subject)
	require.NoError(t, err)
	require.NoError(t, messenger.Publish(subject, wm.NewMessage("test-message", []byte("test"))))
	select {
	case message := <-messages:
		message.Ack()
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	require.NoError(t, messenger.Close())

	report, err = controller.Apply(ctx, manifest, natsadmin.ApplyOptions{DryRun: true})
	require.NoError(t, err)
	require.Contains(t, report.Unchanged, "consumer/"+stream+"/"+consumer)

	until := time.Now().Add(time.Minute)
	paused, err := controller.PauseConsumer(ctx, natsadmin.ConsumerRef{Stream: stream, Consumer: consumer}, natsadmin.PauseRequest{Until: until})
	require.NoError(t, err)
	require.True(t, paused.Paused)
	_, err = controller.ResumeConsumer(ctx, natsadmin.ConsumerRef{Stream: stream, Consumer: consumer})
	require.NoError(t, err)

	testResult, err := controller.PublishTestMessage(ctx, subject)
	require.NoError(t, err)
	require.Equal(t, stream, testResult.Stream)
}
