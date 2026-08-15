// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package client

import (
	"context"
	"reflect"

	"k8s.io/client-go/rest"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"

	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/kubernetes"
)

// New constructs a controller-runtime client. The caller retains ownership of
// config and options; the REST configuration is copied before use.
func New(ctx context.Context, config *rest.Config, options crclient.Options) (crclient.Client, error) {
	if ctx == nil {
		return nil, faults.New(
			faults.CodeInvalidArgument,
			"context is required",
			faults.WithReason("nil_context"),
			faults.WithOperation("kubernetes.client.New"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	if config == nil {
		return nil, faults.New(
			faults.CodeInvalidArgument,
			"Kubernetes REST configuration is required",
			faults.WithReason("nil_rest_configuration"),
			faults.WithOperation("kubernetes.client.New"),
			faults.WithContextMetadata(ctx),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	client, err := crclient.New(rest.CopyConfig(config), options)
	if err != nil {
		return nil, kubernetes.Qualify(ctx, err, "kubernetes.client.New", nil)
	}
	if isNil(client) {
		return nil, faults.New(
			faults.CodeInternal,
			"Kubernetes client constructor returned no client",
			faults.WithReason("nil_kubernetes_client"),
			faults.WithOperation("kubernetes.client.New"),
			faults.WithContextMetadata(ctx),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	return client, nil
}

func isNil(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
