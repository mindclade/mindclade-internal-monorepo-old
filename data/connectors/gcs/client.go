// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package gcs

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	"go.mindclade.dev/data/connectors"
)

const pageSize = 1_000

type ObjectAttrs struct {
	Name       string
	Generation int64
	ETag       string
	Size       int64
	Updated    time.Time
}

// Lister is the narrow shape supplied by a service-owned GCS SDK adapter.
type Lister interface {
	List(context.Context, string, string, string, int) ([]ObjectAttrs, string, error)
}

type Client struct {
	lister Lister
}

func NewClient(lister Lister) (*Client, error) {
	if lister == nil {
		return nil, errors.New("gcs connector lister is required")
	}
	return &Client{lister: lister}, nil
}

func (c *Client) Discover(
	ctx context.Context,
	bucket, prefix, version, licenseRef string,
	cursor connectors.Cursor,
	observedAt time.Time,
) (connectors.Snapshot, error) {
	if c == nil || c.lister == nil || bucket == "" || prefix == "" {
		return connectors.Snapshot{}, errors.New("gcs connector is unconfigured")
	}
	if err := cursor.Validate(); err != nil {
		return connectors.Snapshot{}, err
	}
	objects := make([]connectors.Object, 0)
	pageToken := ""
	for {
		values, next, err := c.lister.List(ctx, bucket, prefix, pageToken, pageSize)
		if err != nil {
			return connectors.Snapshot{}, fmt.Errorf("list gcs source objects: %w", err)
		}
		for _, value := range values {
			if value.Name == "" || value.Generation <= 0 {
				return connectors.Snapshot{}, errors.New("gcs object name/generation is invalid")
			}
			objects = append(objects, connectors.Object{
				URI: "gs://" + bucket + "/" + value.Name, Generation: strconv.FormatInt(value.Generation, 10),
				ETag: value.ETag, SizeBytes: value.Size, UpdatedAt: value.Updated,
			})
			if len(objects) > connectors.MaxObjects {
				return connectors.Snapshot{}, errors.New("gcs source object count exceeds bound")
			}
		}
		if next == "" {
			break
		}
		if next == pageToken {
			return connectors.Snapshot{}, errors.New("gcs pagination cursor did not advance")
		}
		pageToken = next
	}
	slices.SortFunc(objects, func(left, right connectors.Object) int {
		return compare(left.URI+"\x00"+left.Generation, right.URI+"\x00"+right.Generation)
	})
	snapshot := connectors.Snapshot{
		Source: "gcs", Version: version, Cursor: cursor, Objects: objects,
		ObservedAt: observedAt, LicenseRef: licenseRef,
	}
	if err := snapshot.Validate(); err != nil {
		return connectors.Snapshot{}, err
	}
	return snapshot, nil
}

func compare(left, right string) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}
