// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package s3

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"go.mindclade.dev/data/connectors"
)

type ObjectVersion struct {
	Key       string
	VersionID string
	ETag      string
	Size      int64
	Updated   time.Time
}

type Lister interface {
	ListVersions(context.Context, string, string, string, int) ([]ObjectVersion, string, error)
}

type Client struct{ lister Lister }

func NewClient(lister Lister) (*Client, error) {
	if lister == nil {
		return nil, errors.New("s3 connector lister is required")
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
		return connectors.Snapshot{}, errors.New("s3 connector is unconfigured")
	}
	if err := cursor.Validate(); err != nil {
		return connectors.Snapshot{}, err
	}
	objects := make([]connectors.Object, 0)
	continuation := ""
	for {
		values, next, err := c.lister.ListVersions(ctx, bucket, prefix, continuation, 1_000)
		if err != nil {
			return connectors.Snapshot{}, fmt.Errorf("list s3 source object versions: %w", err)
		}
		for _, value := range values {
			if value.Key == "" || value.VersionID == "" {
				return connectors.Snapshot{}, errors.New("s3 object key/version is invalid")
			}
			objects = append(objects, connectors.Object{
				URI: "s3://" + bucket + "/" + value.Key, Generation: value.VersionID,
				ETag: value.ETag, SizeBytes: value.Size, UpdatedAt: value.Updated,
			})
			if len(objects) > connectors.MaxObjects {
				return connectors.Snapshot{}, errors.New("s3 source object count exceeds bound")
			}
		}
		if next == "" {
			break
		}
		if next == continuation {
			return connectors.Snapshot{}, errors.New("s3 pagination cursor did not advance")
		}
		continuation = next
	}
	slices.SortFunc(objects, func(left, right connectors.Object) int {
		return compare(left.URI+"\x00"+left.Generation, right.URI+"\x00"+right.Generation)
	})
	snapshot := connectors.Snapshot{
		Source: "s3", Version: version, Cursor: cursor, Objects: objects,
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
