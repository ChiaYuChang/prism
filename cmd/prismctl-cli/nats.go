package main

import (
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/ChiaYuChang/prism/internal/infra/natsadmin"
	"github.com/ChiaYuChang/prism/internal/infra/natsdiag"
	"github.com/spf13/cobra"
)

func adminNATSStatusCommand(ctx *cliContext) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show read-only NATS JetStream status",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return requestNATSSnapshot(ctx, cmd, "nats_status", func(snapshot natsdiag.Snapshot) any {
				return snapshot
			})
		},
	}
}

func adminNATSStreamsCommand(ctx *cliContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "streams",
		Short: "List and manage NATS JetStream streams",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return requestNATSSnapshot(ctx, cmd, "nats_streams", func(snapshot natsdiag.Snapshot) any {
				return snapshot.Streams
			})
		},
	}
	cmd.AddCommand(adminNATSStreamPurgeCommand(ctx), adminNATSStreamDeleteCommand(ctx))
	return cmd
}

func adminNATSConsumersCommand(ctx *cliContext) *cobra.Command {
	var stream string
	cmd := &cobra.Command{
		Use:   "consumers",
		Short: "List and manage NATS JetStream consumers",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return requestNATSSnapshot(ctx, cmd, "nats_consumers", func(snapshot natsdiag.Snapshot) any {
				result := make([]natsdiag.ConsumerInfo, 0)
				for _, item := range snapshot.Streams {
					if stream != "" && item.Name != stream {
						continue
					}
					result = append(result, item.Consumers...)
				}
				return result
			})
		},
	}
	cmd.Flags().StringVar(&stream, "stream", "", "Filter by stream name")
	cmd.AddCommand(adminNATSConsumerPauseCommand(ctx), adminNATSConsumerResumeCommand(ctx), adminNATSConsumerDeleteCommand(ctx))
	return cmd
}

func adminNATSApplyCommand(ctx *cliContext) *cobra.Command {
	var manifestPath string
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Create or update JetStream resources from a manifest",
		RunE: func(cmd *cobra.Command, _ []string) error {
			data, err := os.ReadFile(manifestPath)
			if err != nil {
				return err
			}
			manifest, err := natsadmin.ParseManifest(data)
			if err != nil {
				return err
			}
			a, warnings, err := ctx.sourceAPI()
			if err != nil {
				return err
			}
			var report natsadmin.ApplyReport
			body := map[string]any{"manifest": manifest, "dry_run": dryRun}
			if err := a.request(cmd.Context(), http.MethodPost, "/admin/nats/apply", body, &report); err != nil {
				return renderError(ctx, "admin", "nats_apply", err, warnings)
			}
			return render(ctx, "admin", "nats_apply", report, warnings)
		},
	}
	cmd.Flags().StringVar(&manifestPath, "manifest", "configs/registry/jetstream.yaml", "JetStream manifest path")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Validate and report changes without applying them")
	return cmd
}

func adminNATSConsumerPauseCommand(ctx *cliContext) *cobra.Command {
	var stream, consumer, untilRaw, reason string
	cmd := &cobra.Command{
		Use:   "pause",
		Short: "Pause delivery to a JetStream consumer",
		RunE: func(cmd *cobra.Command, _ []string) error {
			until, err := time.Parse(time.RFC3339, untilRaw)
			if err != nil {
				return err
			}
			var result natsadmin.PauseResult
			if err := requestNATSMutation(ctx, cmd, http.MethodPost, consumerPath(stream, consumer)+"/pause", map[string]any{"until": until, "reason": reason}, &result); err != nil {
				return err
			}
			return render(ctx, "admin", "nats_consumer_pause", result, nil)
		},
	}
	cmd.Flags().StringVar(&stream, "stream", "", "Stream name")
	cmd.Flags().StringVar(&consumer, "consumer", "", "Consumer name")
	cmd.Flags().StringVar(&untilRaw, "until", "", "Pause deadline in RFC3339 format")
	cmd.Flags().StringVar(&reason, "reason", "", "Operator reason")
	_ = cmd.MarkFlagRequired("stream")
	_ = cmd.MarkFlagRequired("consumer")
	_ = cmd.MarkFlagRequired("until")
	return cmd
}

func adminNATSConsumerResumeCommand(ctx *cliContext) *cobra.Command {
	var stream, consumer string
	cmd := &cobra.Command{
		Use:   "resume",
		Short: "Resume delivery to a JetStream consumer",
		RunE: func(cmd *cobra.Command, _ []string) error {
			var result natsadmin.PauseResult
			if err := requestNATSMutation(ctx, cmd, http.MethodPost, consumerPath(stream, consumer)+"/resume", nil, &result); err != nil {
				return err
			}
			return render(ctx, "admin", "nats_consumer_resume", result, nil)
		},
	}
	cmd.Flags().StringVar(&stream, "stream", "", "Stream name")
	cmd.Flags().StringVar(&consumer, "consumer", "", "Consumer name")
	_ = cmd.MarkFlagRequired("stream")
	_ = cmd.MarkFlagRequired("consumer")
	return cmd
}

func adminNATSConsumerDeleteCommand(ctx *cliContext) *cobra.Command {
	var stream, consumer, confirm string
	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete a JetStream consumer",
		RunE: func(cmd *cobra.Command, _ []string) error {
			path := consumerPath(stream, consumer) + "?confirm=" + url.QueryEscape(confirm)
			if err := requestNATSMutation(ctx, cmd, http.MethodDelete, path, nil, nil); err != nil {
				return err
			}
			return render(ctx, "admin", "nats_consumer_delete", map[string]string{"stream": stream, "consumer": consumer}, nil)
		},
	}
	cmd.Flags().StringVar(&stream, "stream", "", "Stream name")
	cmd.Flags().StringVar(&consumer, "consumer", "", "Consumer name")
	cmd.Flags().StringVar(&confirm, "confirm", "", "Repeat the consumer name to confirm deletion")
	_ = cmd.MarkFlagRequired("stream")
	_ = cmd.MarkFlagRequired("consumer")
	_ = cmd.MarkFlagRequired("confirm")
	return cmd
}

func adminNATSStreamPurgeCommand(ctx *cliContext) *cobra.Command {
	return destructiveNATSStreamCommand(ctx, "purge", http.MethodPost, "Purge all messages from a JetStream stream")
}

func adminNATSStreamDeleteCommand(ctx *cliContext) *cobra.Command {
	return destructiveNATSStreamCommand(ctx, "delete", http.MethodDelete, "Delete a JetStream stream and its consumers")
}

func destructiveNATSStreamCommand(ctx *cliContext, action, method, short string) *cobra.Command {
	var stream, confirm string
	cmd := &cobra.Command{
		Use:   action,
		Short: short,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path := "/admin/nats/streams/" + url.PathEscape(stream)
			if action == "purge" {
				path += "/purge"
			}
			path += "?confirm=" + url.QueryEscape(confirm)
			if err := requestNATSMutation(ctx, cmd, method, path, nil, nil); err != nil {
				return err
			}
			return render(ctx, "admin", "nats_stream_"+action, map[string]string{"stream": stream}, nil)
		},
	}
	cmd.Flags().StringVar(&stream, "stream", "", "Stream name")
	cmd.Flags().StringVar(&confirm, "confirm", "", "Repeat the stream name to confirm the operation")
	_ = cmd.MarkFlagRequired("stream")
	_ = cmd.MarkFlagRequired("confirm")
	return cmd
}

func adminNATSTestMessageCommand(ctx *cliContext) *cobra.Command {
	var subject string
	cmd := &cobra.Command{
		Use:   "test-message",
		Short: "Publish a diagnostic message to a test-enabled stream",
		RunE: func(cmd *cobra.Command, _ []string) error {
			var result natsadmin.TestMessageResult
			if err := requestNATSMutation(ctx, cmd, http.MethodPost, "/admin/nats/test-message", map[string]string{"subject": subject}, &result); err != nil {
				return err
			}
			return render(ctx, "admin", "nats_test_message", result, nil)
		},
	}
	cmd.Flags().StringVar(&subject, "subject", "", "Test-enabled NATS subject")
	_ = cmd.MarkFlagRequired("subject")
	return cmd
}

func requestNATSMutation(ctx *cliContext, cmd *cobra.Command, method, path string, body, out any) error {
	a, warnings, err := ctx.sourceAPI()
	if err != nil {
		return err
	}
	if err := a.request(cmd.Context(), method, path, body, out); err != nil {
		return renderError(ctx, "admin", "nats_mutation", err, warnings)
	}
	return nil
}

func consumerPath(stream, consumer string) string {
	return "/admin/nats/streams/" + url.PathEscape(stream) + "/consumers/" + url.PathEscape(consumer)
}

func requestNATSSnapshot(ctx *cliContext, cmd *cobra.Command, action string, shape func(natsdiag.Snapshot) any) error {
	a, warnings, err := ctx.sourceAPI()
	if err != nil {
		return err
	}
	var snapshot natsdiag.Snapshot
	if err := a.request(cmd.Context(), http.MethodGet, "/admin/nats", nil, &snapshot); err != nil {
		return renderError(ctx, "admin", action, err, warnings)
	}
	result := shape(snapshot)
	return render(ctx, "admin", action, result, warnings)
}
