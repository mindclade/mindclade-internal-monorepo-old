// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package client

import (
	"context"

	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"

	"mindclade.internal/libs/go/faults"
	mckubernetes "mindclade.internal/libs/go/kubernetes"
)

func NewDiscovery(ctx context.Context, config *rest.Config) (discovery.DiscoveryInterface, error) {
	const operation = "kubernetes.client.NewDiscovery"
	if ctx == nil || config == nil {
		return nil, invalidFactory(ctx, operation)
	}
	client, err := discovery.NewDiscoveryClientForConfig(rest.CopyConfig(config))
	if err != nil {
		return nil, mckubernetes.Qualify(ctx, err, operation, nil)
	}
	if isNil(client) {
		return nil, nilFactory(ctx, operation)
	}
	return client, nil
}

// VersionDiscovery is the narrow discovery capability needed by Probe.
// discovery.DiscoveryInterface satisfies this contract.
type VersionDiscovery interface {
	ServerVersion() (*version.Info, error)
}

// Probe verifies API-server reachability and returns its version metadata.
func Probe(ctx context.Context, client VersionDiscovery) (*version.Info, error) {
	const operation = "kubernetes.client.Probe"
	if ctx == nil || isNil(client) {
		return nil, invalidFactory(ctx, operation)
	}
	select {
	case <-ctx.Done():
		return nil, mckubernetes.Qualify(ctx, ctx.Err(), operation, nil)
	default:
	}
	info, err := client.ServerVersion()
	if err != nil {
		return nil, mckubernetes.Qualify(ctx, err, operation, nil)
	}
	if info == nil {
		return nil, faults.New(faults.CodeDataLoss, "Kubernetes API returned no version information", faults.WithReason("nil_server_version"), faults.WithOperation(operation), faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return info, nil
}
