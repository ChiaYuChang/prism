package natsadmin

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const ManifestVersion = 1

var (
	ErrInvalidManifest  = errors.New("invalid JetStream manifest")
	resourceNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
)

type Manifest struct {
	Version   int                `json:"version" yaml:"version"`
	Streams   []StreamManifest   `json:"streams" yaml:"streams"`
	Consumers []ConsumerManifest `json:"consumers" yaml:"consumers"`
}

type StreamManifest struct {
	Name             string   `json:"name" yaml:"name"`
	Subjects         []string `json:"subjects" yaml:"subjects"`
	Storage          string   `json:"storage" yaml:"storage"`
	Retention        string   `json:"retention" yaml:"retention"`
	Replicas         int      `json:"replicas" yaml:"replicas"`
	MaxAge           string   `json:"max_age,omitempty" yaml:"max_age,omitempty"`
	MaxMessages      int64    `json:"max_messages,omitempty" yaml:"max_messages,omitempty"`
	AllowTestPublish bool     `json:"allow_test_publish,omitempty" yaml:"allow_test_publish,omitempty"`
}

type ConsumerManifest struct {
	Stream        string `json:"stream" yaml:"stream"`
	Name          string `json:"name" yaml:"name"`
	FilterSubject string `json:"filter_subject" yaml:"filter_subject"`
	QueueGroup    string `json:"queue_group" yaml:"queue_group"`
	DeliverPolicy string `json:"deliver_policy" yaml:"deliver_policy"`
	AckPolicy     string `json:"ack_policy" yaml:"ack_policy"`
	AckWait       string `json:"ack_wait" yaml:"ack_wait"`
	MaxAckPending int    `json:"max_ack_pending" yaml:"max_ack_pending"`
	MaxDeliver    int    `json:"max_deliver" yaml:"max_deliver"`
}

func ParseManifest(data []byte) (Manifest, error) {
	var manifest Manifest
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("%w: decode: %v", ErrInvalidManifest, err)
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func (m Manifest) Validate() error {
	if m.Version != ManifestVersion {
		return fmt.Errorf("%w: version must be %d", ErrInvalidManifest, ManifestVersion)
	}
	streams := make(map[string]StreamManifest, len(m.Streams))
	for i, stream := range m.Streams {
		if !resourceNamePattern.MatchString(stream.Name) {
			return fmt.Errorf("%w: streams[%d].name", ErrInvalidManifest, i)
		}
		if _, exists := streams[stream.Name]; exists {
			return fmt.Errorf("%w: duplicate stream %q", ErrInvalidManifest, stream.Name)
		}
		if len(stream.Subjects) == 0 {
			return fmt.Errorf("%w: stream %q has no subjects", ErrInvalidManifest, stream.Name)
		}
		if stream.Storage != "file" && stream.Storage != "memory" {
			return fmt.Errorf("%w: stream %q storage must be file or memory", ErrInvalidManifest, stream.Name)
		}
		if stream.Retention != "limits" && stream.Retention != "interest" && stream.Retention != "workqueue" {
			return fmt.Errorf("%w: stream %q retention is invalid", ErrInvalidManifest, stream.Name)
		}
		if stream.Replicas < 1 || stream.Replicas > 5 {
			return fmt.Errorf("%w: stream %q replicas must be between 1 and 5", ErrInvalidManifest, stream.Name)
		}
		if stream.MaxAge != "" {
			if _, err := time.ParseDuration(stream.MaxAge); err != nil {
				return fmt.Errorf("%w: stream %q max_age: %v", ErrInvalidManifest, stream.Name, err)
			}
		}
		if stream.MaxMessages < 0 {
			return fmt.Errorf("%w: stream %q max_messages must not be negative", ErrInvalidManifest, stream.Name)
		}
		if stream.AllowTestPublish {
			for _, subject := range stream.Subjects {
				if subject != "prism_test" && !strings.HasPrefix(subject, "prism_test.") {
					return fmt.Errorf("%w: stream %q test publishing requires prism_test subjects", ErrInvalidManifest, stream.Name)
				}
			}
		}
		streams[stream.Name] = stream
	}
	consumers := make(map[string]struct{}, len(m.Consumers))
	for i, consumer := range m.Consumers {
		if !resourceNamePattern.MatchString(consumer.Name) {
			return fmt.Errorf("%w: consumers[%d].name", ErrInvalidManifest, i)
		}
		stream, exists := streams[consumer.Stream]
		if !exists {
			return fmt.Errorf("%w: consumer %q references unknown stream %q", ErrInvalidManifest, consumer.Name, consumer.Stream)
		}
		key := consumer.Stream + "/" + consumer.Name
		if _, exists := consumers[key]; exists {
			return fmt.Errorf("%w: duplicate consumer %q", ErrInvalidManifest, key)
		}
		if consumer.QueueGroup == "" || consumer.FilterSubject == "" {
			return fmt.Errorf("%w: consumer %q requires queue_group and filter_subject", ErrInvalidManifest, key)
		}
		if !subjectCovered(consumer.FilterSubject, stream.Subjects) {
			return fmt.Errorf("%w: consumer %q filter is not covered by stream", ErrInvalidManifest, key)
		}
		if consumer.DeliverPolicy != "all" || consumer.AckPolicy != "explicit" {
			return fmt.Errorf("%w: consumer %q requires deliver_policy=all and ack_policy=explicit", ErrInvalidManifest, key)
		}
		if _, err := time.ParseDuration(consumer.AckWait); err != nil {
			return fmt.Errorf("%w: consumer %q ack_wait: %v", ErrInvalidManifest, key, err)
		}
		if consumer.MaxAckPending < 1 || consumer.MaxDeliver < 1 {
			return fmt.Errorf("%w: consumer %q limits must be positive", ErrInvalidManifest, key)
		}
		consumers[key] = struct{}{}
	}
	return nil
}

func subjectCovered(subject string, patterns []string) bool {
	for _, pattern := range patterns {
		if pattern == subject || pattern == ">" {
			return true
		}
		if strings.HasSuffix(pattern, ".>") && strings.HasPrefix(subject, strings.TrimSuffix(pattern, ">")) {
			return true
		}
		if strings.HasSuffix(pattern, ".*") {
			prefix := strings.TrimSuffix(pattern, "*")
			if strings.HasPrefix(subject, prefix) && !strings.Contains(strings.TrimPrefix(subject, prefix), ".") {
				return true
			}
		}
	}
	return false
}
