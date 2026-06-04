package appconfig

import (
	"context"
	"fmt"
	"log/slog"

	prismlogger "github.com/ChiaYuChang/prism/pkg/logger"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3Config carries credentials and endpoint overrides for the AWS S3 client.
// Bucket and prefix live in the archive URI (s3://bucket/prefix), not here.
// All fields are optional: when AccessKey/SecretKey are empty the SDK default
// credential chain is used (env, shared config, IAM role, etc).
type S3Config struct {
	Endpoint     string `mapstructure:"endpoint"`
	Region       string `mapstructure:"region"`
	AccessKey    string `mapstructure:"access-key"`
	SecretKey    string `mapstructure:"secret-key"`
	UsePathStyle bool   `mapstructure:"use-path-style"`
}

// String renders a human-readable summary with S3 credentials redacted.
func (c S3Config) String() string {
	return fmt.Sprintf("endpoint=%s region=%s access_key=%s secret_key=%s use_path_style=%t",
		c.Endpoint, c.Region, prismlogger.SecretMask(c.AccessKey), prismlogger.SecretMask(c.SecretKey), c.UsePathStyle)
}

// LogValue redacts S3 credentials when logged via slog.Any.
func (c S3Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("endpoint", c.Endpoint),
		slog.String("region", c.Region),
		slog.String("access_key", prismlogger.SecretMask(c.AccessKey)),
		slog.String("secret_key", prismlogger.SecretMask(c.SecretKey)),
		slog.Bool("use_path_style", c.UsePathStyle),
	)
}

func (c S3Config) NewClient(ctx context.Context) (*s3.Client, error) {
	opts := []func(*config.LoadOptions) error{
		config.WithRegion(c.Region),
	}
	if c.AccessKey != "" && c.SecretKey != "" {
		opts = append(opts, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(c.AccessKey, c.SecretKey, "")))
	}
	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("load s3 config: %w", err)
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.UsePathStyle = c.UsePathStyle
		if c.Endpoint != "" {
			o.BaseEndpoint = aws.String(c.Endpoint)
		}
	})

	return client, nil
}
