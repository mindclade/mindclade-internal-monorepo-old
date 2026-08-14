// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package pubsub

import (
	"strings"
	"time"

	"mindclade.internal/libs/go/faults"
)

const (
	DefaultMaxConcurrentHandlers = 16
	DefaultAckDeadline           = 30 * time.Second
)

type Config struct {
	Topic                 string
	MaxConcurrentHandlers int
	AckDeadline           time.Duration
}

func (config Config) normalized() (Config, error) {
	config.Topic = strings.TrimSpace(config.Topic)
	if config.MaxConcurrentHandlers == 0 {
		config.MaxConcurrentHandlers = DefaultMaxConcurrentHandlers
	}
	if config.AckDeadline == 0 {
		config.AckDeadline = DefaultAckDeadline
	}
	if config.Topic == "" || config.MaxConcurrentHandlers < 1 || config.AckDeadline <= 0 {
		return Config{}, faults.New(
			faults.CodeInvalidArgument,
			"invalid Pub/Sub adapter configuration",
			faults.WithReason("invalid_pubsub_config"),
			faults.WithOperation("messaging.pubsub.Config.Validate"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	return config, nil
}
