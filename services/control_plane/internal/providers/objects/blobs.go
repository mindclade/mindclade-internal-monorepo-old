// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package objects binds the artifact store and the read cache: the two
// provider-backed stores whose clients own a lifetime of their own.
//
// It is a sibling of the composition root rather than part of it so that a
// role links only what it uses. The registry holds artifacts and the ingestion
// coordinator stages them; no other role should carry a Cloud Storage or Redis
// client into its binary.
package objects

import (
	"context"
	"strings"

	gcsapi "cloud.google.com/go/storage"

	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/servicekit"
	"go.mindclade.dev/libs/go/storage/blob"
	"go.mindclade.dev/libs/go/storage/blob/gcs"
	"go.mindclade.dev/services/control_plane/internal/config"
)

// newBlobStore builds the Google Cloud Storage adapter and the component that
// owns the client's lifetime. The store itself is stateless; only the client
// needs shutdown, so it is staged as infrastructure and stopped in reverse.
func NewBlobStore(ctx context.Context, settings config.Settings) (blob.Store, servicekit.Component, error) {
	bucket := strings.TrimSpace(settings.BlobBucket)
	if bucket == "" {
		return nil, servicekit.Component{}, faults.New(
			faults.CodeFailedPrecondition,
			"control-plane blob bucket is not configured",
			faults.WithReason("blob_bucket_not_configured"),
			faults.WithOperation("controlplane.objects.NewBlobStore"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	client, err := gcsapi.NewClient(ctx)
	if err != nil {
		return nil, servicekit.Component{}, faults.Wrap(err, faults.CodeUnavailable,
			"unable to create the Google Cloud Storage client",
			faults.WithReason("blob_client_failed"),
			faults.WithOperation("controlplane.objects.NewBlobStore"),
		)
	}
	options := make([]gcs.Option, 0, 1)
	if prefix := strings.TrimSpace(settings.BlobPrefix); prefix != "" {
		options = append(options, gcs.WithPrefix(prefix))
	}
	store, err := gcs.New(client, bucket, options...)
	if err != nil {
		_ = client.Close()
		return nil, servicekit.Component{}, err
	}
	component := servicekit.Component{
		Name: "gcs-client",
		Stop: func(context.Context) error {
			if err := client.Close(); err != nil {
				return faults.Wrap(err, faults.CodeInternal,
					"unable to close the Google Cloud Storage client",
					faults.WithReason("blob_client_close_failed"),
					faults.WithOperation("controlplane.objects.blobComponent.Stop"),
				)
			}
			return nil
		},
	}
	return store, component, nil
}
