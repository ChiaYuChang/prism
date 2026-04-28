package appconfig

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
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

func (c *S3Config) BindFlags(v *viper.Viper, fs *pflag.FlagSet) error {
	fs.String("s3-endpoint", "", "S3 endpoint URL (leave empty for AWS; set for SeaweedFS/MinIO e.g. http://localhost:8333)")
	fs.String("s3-region", "us-east-1", "S3 region")
	fs.String("s3-access-key", "", "S3 access key (empty uses AWS SDK default credential chain)")
	fs.String("s3-secret-key", "", "S3 secret key (empty uses AWS SDK default credential chain)")
	fs.Bool("s3-use-path-style", true, "Use path style addressing (required for SeaweedFS/MinIO)")

	if err := v.BindPFlags(fs); err != nil {
		return err
	}
	return nil
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
