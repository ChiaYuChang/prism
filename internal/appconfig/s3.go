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

type S3Config struct {
	Endpoint     string `mapstructure:"endpoint"        validate:"required"`
	Region       string `mapstructure:"region"`
	Bucket       string `mapstructure:"bucket"          validate:"required"`
	AccessKey    string `mapstructure:"access-key"      validate:"required"`
	SecretKey    string `mapstructure:"secret-key"      validate:"required"`
	UsePathStyle bool   `mapstructure:"use-path-style"`
}

func (c *S3Config) BindFlags(v *viper.Viper, fs *pflag.FlagSet) error {
	fs.String("s3-endpoint", "http://localhost:8333", "S3/SeaweedFS endpoint URL")
	fs.String("s3-region", "us-east-1", "S3 region")
	fs.String("s3-bucket", "prism-archive", "S3 bucket name")
	fs.String("s3-access-key", "any", "S3 access key")
	fs.String("s3-secret-key", "any", "S3 secret key")
	fs.Bool("s3-use-path-style", true, "Use path style addressing (required for SeaweedFS/MinIO)")

	if err := v.BindPFlags(fs); err != nil {
		return err
	}
	return nil
}

func (c S3Config) NewClient(ctx context.Context) (*s3.Client, error) {
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(c.Region),
		config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(c.AccessKey, c.SecretKey, "")),
	)
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
