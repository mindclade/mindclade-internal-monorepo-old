// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package huggingface

import (
	"context"
	"errors"

	"go.mindclade.dev/data/connectors"
)

type ManifestFetcher interface {
	FetchSnapshot(context.Context, string) (connectors.Snapshot, error)
}

type Client struct{ fetcher ManifestFetcher }

func NewClient(fetcher ManifestFetcher) (*Client, error) {
	if fetcher == nil {
		return nil, errors.New("huggingface manifest fetcher is required")
	}
	return &Client{fetcher: fetcher}, nil
}

func (c *Client) Discover(ctx context.Context, manifestURL string) (connectors.Snapshot, error) {
	if c == nil || c.fetcher == nil {
		return connectors.Snapshot{}, errors.New("huggingface connector is unconfigured")
	}
	snapshot, err := c.fetcher.FetchSnapshot(ctx, manifestURL)
	if err != nil {
		return connectors.Snapshot{}, err
	}
	if snapshot.Source != "huggingface" {
		return connectors.Snapshot{}, errors.New("huggingface manifest source does not match connector")
	}
	if err := snapshot.Validate(); err != nil {
		return connectors.Snapshot{}, err
	}
	return snapshot, nil
}
