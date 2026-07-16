package natsadmin_test

import (
	"testing"

	"github.com/ChiaYuChang/prism/internal/infra/natsadmin"
	"github.com/stretchr/testify/require"
)

func TestParseManifest(t *testing.T) {
	manifest, err := natsadmin.ParseManifest([]byte(`
version: 1
streams:
  - name: prism_task
    subjects: [prism_task]
    storage: file
    retention: limits
    replicas: 1
consumers:
  - stream: prism_task
    name: discovery-worker
    filter_subject: prism_task
    queue_group: discovery-worker
    deliver_policy: all
    ack_policy: explicit
    ack_wait: 30s
    max_ack_pending: 16
    max_deliver: 8
`))
	require.NoError(t, err)
	require.Len(t, manifest.Streams, 1)
	require.Len(t, manifest.Consumers, 1)
}

func TestParseManifestRejectsUnknownField(t *testing.T) {
	_, err := natsadmin.ParseManifest([]byte(`version: 1
unknown: true
`))
	require.ErrorIs(t, err, natsadmin.ErrInvalidManifest)
}

func TestManifestRejectsUnknownConsumerStream(t *testing.T) {
	err := (natsadmin.Manifest{Version: 1, Consumers: []natsadmin.ConsumerManifest{{Stream: "missing", Name: "worker"}}}).Validate()
	require.ErrorIs(t, err, natsadmin.ErrInvalidManifest)
}

func TestManifestRestrictsTestPublishingNamespace(t *testing.T) {
	err := (natsadmin.Manifest{Version: 1, Streams: []natsadmin.StreamManifest{{
		Name: "prism_task", Subjects: []string{"prism_task"}, Storage: "file", Retention: "limits", Replicas: 1, AllowTestPublish: true,
	}}}).Validate()
	require.ErrorIs(t, err, natsadmin.ErrInvalidManifest)
}
