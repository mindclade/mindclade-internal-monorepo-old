// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package client

import (
	"context"

	"k8s.io/client-go/dynamic"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"go.mindclade.dev/libs/go/faults"
	mckubernetes "go.mindclade.dev/libs/go/kubernetes"
)

func NewTyped(ctx context.Context, config *rest.Config) (clientset.Interface, error) {
	const operation = "kubernetes.client.NewTyped"
	if ctx == nil || config == nil {
		return nil, invalidFactory(ctx, operation)
	}
	client, err := clientset.NewForConfig(rest.CopyConfig(config))
	if err != nil {
		return nil, mckubernetes.Qualify(ctx, err, operation, nil)
	}
	if isNil(client) {
		return nil, nilFactory(ctx, operation)
	}
	return client, nil
}

func NewDynamic(ctx context.Context, config *rest.Config) (dynamic.Interface, error) {
	const operation = "kubernetes.client.NewDynamic"
	if ctx == nil || config == nil {
		return nil, invalidFactory(ctx, operation)
	}
	client, err := dynamic.NewForConfig(rest.CopyConfig(config))
	if err != nil {
		return nil, mckubernetes.Qualify(ctx, err, operation, nil)
	}
	if isNil(client) {
		return nil, nilFactory(ctx, operation)
	}
	return client, nil
}

func invalidFactory(ctx context.Context, operation string) error {
	options := []faults.Option{faults.WithReason("invalid_kubernetes_client_request"), faults.WithOperation(operation), faults.WithRetryPolicy(faults.NoRetry())}
	if ctx != nil {
		options = append(options, faults.WithContextMetadata(ctx))
	}
	return faults.New(faults.CodeInvalidArgument, "invalid Kubernetes client request", options...)
}

func nilFactory(ctx context.Context, operation string) error {
	return faults.New(faults.CodeInternal, "Kubernetes client constructor returned no client", faults.WithReason("nil_kubernetes_client"), faults.WithOperation(operation), faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
}
