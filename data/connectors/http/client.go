// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	nethttp "net/http"
	"net/url"
	"strings"

	"go.mindclade.dev/data/connectors"
)

const defaultMaxManifestBytes int64 = 16 * 1024 * 1024

type Client struct {
	http         *nethttp.Client
	allowedHosts map[string]struct{}
	maxBytes     int64
}

func NewClient(base *nethttp.Client, allowedHosts []string, maxBytes int64) (*Client, error) {
	if base == nil || len(allowedHosts) == 0 {
		return nil, errors.New("http connector client and allowed hosts are required")
	}
	if maxBytes == 0 {
		maxBytes = defaultMaxManifestBytes
	}
	if maxBytes < 1 || maxBytes > 256*1024*1024 {
		return nil, errors.New("http connector manifest byte limit is invalid")
	}
	hosts := make(map[string]struct{}, len(allowedHosts))
	for _, host := range allowedHosts {
		if host == "" || strings.ContainsAny(host, "/@?#") {
			return nil, errors.New("http connector allowed host is invalid")
		}
		hosts[strings.ToLower(host)] = struct{}{}
	}
	client := *base
	client.CheckRedirect = func(_ *nethttp.Request, _ []*nethttp.Request) error {
		return errors.New("http connector redirects are disabled")
	}
	return &Client{http: &client, allowedHosts: hosts, maxBytes: maxBytes}, nil
}

func (c *Client) FetchSnapshot(ctx context.Context, endpoint string) (connectors.Snapshot, error) {
	if c == nil || c.http == nil {
		return connectors.Snapshot{}, errors.New("http connector is unconfigured")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.Fragment != "" {
		return connectors.Snapshot{}, errors.New("http connector endpoint must be credential-free HTTPS")
	}
	if _, ok := c.allowedHosts[strings.ToLower(parsed.Host)]; !ok {
		return connectors.Snapshot{}, errors.New("http connector endpoint host is not allowed")
	}
	request, err := nethttp.NewRequestWithContext(ctx, nethttp.MethodGet, endpoint, nil)
	if err != nil {
		return connectors.Snapshot{}, err
	}
	request.Header.Set("Accept", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return connectors.Snapshot{}, fmt.Errorf("fetch connector snapshot: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != nethttp.StatusOK {
		return connectors.Snapshot{}, fmt.Errorf("connector snapshot returned HTTP %d", response.StatusCode)
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, c.maxBytes+1))
	if err != nil {
		return connectors.Snapshot{}, fmt.Errorf("read connector snapshot: %w", err)
	}
	if int64(len(payload)) > c.maxBytes {
		return connectors.Snapshot{}, errors.New("connector snapshot exceeds byte limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var snapshot connectors.Snapshot
	if err := decoder.Decode(&snapshot); err != nil {
		return connectors.Snapshot{}, fmt.Errorf("decode connector snapshot: %w", err)
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		return connectors.Snapshot{}, errors.New("connector snapshot contains trailing data")
	}
	if err := snapshot.Validate(); err != nil {
		return connectors.Snapshot{}, err
	}
	return snapshot, nil
}
