// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package client

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"mindclade.internal/libs/go/faults"
)

type Source string

const (
	SourceAuto       Source = "auto"
	SourceInCluster  Source = "in_cluster"
	SourceKubeconfig Source = "kubeconfig"
)

// Config describes how to discover and tune a Kubernetes REST configuration.
type Config struct {
	Source         Source
	KubeconfigPath string
	Context        string
	Cluster        string
	User           string
	UserAgent      string
	QPS            float32
	Burst          int
	Timeout        time.Duration
}

func DefaultConfig() Config {
	return Config{
		Source: SourceAuto,
		QPS:    20,
		Burst:  30,
	}
}

func (config Config) Validate() error {
	switch config.Source {
	case "", SourceAuto, SourceInCluster, SourceKubeconfig:
	default:
		return invalid("invalid Kubernetes configuration source", "invalid_configuration_source", faults.Fields{"source": config.Source})
	}
	if config.QPS < 0 {
		return invalid("Kubernetes client QPS cannot be negative", "negative_client_qps", nil)
	}
	if config.Burst < 0 {
		return invalid("Kubernetes client burst cannot be negative", "negative_client_burst", nil)
	}
	if config.Timeout < 0 {
		return invalid("Kubernetes client timeout cannot be negative", "negative_client_timeout", nil)
	}
	if config.Source == SourceInCluster && strings.TrimSpace(config.KubeconfigPath) != "" {
		return invalid("in-cluster configuration cannot specify a kubeconfig path", "in_cluster_with_kubeconfig", nil)
	}
	return nil
}

// Load discovers and returns an independent REST configuration.
func Load(ctx context.Context, config Config) (*rest.Config, error) {
	if ctx == nil {
		return nil, invalid("context is required", "nil_context", nil)
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, faults.Wrap(
			ctx.Err(),
			faults.CodeOf(ctx.Err()),
			"Kubernetes configuration loading was interrupted",
			faults.WithReason("configuration_loading_interrupted"),
			faults.WithOperation("kubernetes.client.Load"),
			faults.WithContextMetadata(ctx),
		)
	default:
	}

	source := config.Source
	if source == "" {
		source = SourceAuto
	}
	var restConfig *rest.Config
	var err error
	switch source {
	case SourceInCluster:
		restConfig, err = rest.InClusterConfig()
	case SourceKubeconfig:
		restConfig, err = loadKubeconfig(config)
	case SourceAuto:
		if strings.TrimSpace(config.KubeconfigPath) != "" {
			restConfig, err = loadKubeconfig(config)
			break
		}
		restConfig, err = rest.InClusterConfig()
		if err != nil {
			var kubeconfigErr error
			restConfig, kubeconfigErr = loadKubeconfig(config)
			if kubeconfigErr != nil {
				err = errors.Join(err, kubeconfigErr)
			} else {
				err = nil
			}
		}
	}
	if err != nil {
		return nil, faults.Wrap(
			err,
			faults.CodeFailedPrecondition,
			"Kubernetes client configuration is unavailable",
			faults.WithReason("kubernetes_configuration_unavailable"),
			faults.WithOperation("kubernetes.client.Load"),
			faults.WithContextMetadata(ctx),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	if restConfig == nil {
		return nil, faults.New(
			faults.CodeInternal,
			"Kubernetes configuration loader returned no configuration",
			faults.WithReason("nil_rest_configuration"),
			faults.WithOperation("kubernetes.client.Load"),
			faults.WithContextMetadata(ctx),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}

	configured := rest.CopyConfig(restConfig)
	if config.UserAgent != "" {
		configured.UserAgent = strings.TrimSpace(config.UserAgent)
	}
	if config.QPS > 0 {
		configured.QPS = config.QPS
	}
	if config.Burst > 0 {
		configured.Burst = config.Burst
	}
	if config.Timeout > 0 {
		configured.Timeout = config.Timeout
	}
	return configured, nil
}

func loadKubeconfig(config Config) (*rest.Config, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if explicitPath := strings.TrimSpace(config.KubeconfigPath); explicitPath != "" {
		rules.ExplicitPath = explicitPath
	}
	overrides := &clientcmd.ConfigOverrides{
		CurrentContext: strings.TrimSpace(config.Context),
	}
	overrides.Context.Cluster = strings.TrimSpace(config.Cluster)
	overrides.Context.AuthInfo = strings.TrimSpace(config.User)
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides).ClientConfig()
}

func invalid(message, reason string, fields faults.Fields) error {
	return faults.New(
		faults.CodeInvalidArgument,
		message,
		faults.WithReason(reason),
		faults.WithOperation("kubernetes.client.Config.Validate"),
		faults.WithFields(fields),
		faults.WithRetryPolicy(faults.NoRetry()),
	)
}

func (source Source) String() string { return string(source) }

func (config Config) String() string {
	return fmt.Sprintf("source=%s context=%q cluster=%q", config.Source, config.Context, config.Cluster)
}
