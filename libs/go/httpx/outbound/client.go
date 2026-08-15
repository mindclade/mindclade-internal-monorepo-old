// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package outbound

import (
	"compress/gzip"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/httpx"
)

// Client is a policy-enforcing HTTP client. Callers cannot replace its
// transport or redirect policy after construction.
type Client struct {
	policy      Policy
	resolver    Resolver
	http        *http.Client
	nextAddress atomic.Uint64
}

func NewClient(policy Policy) (*Client, error) {
	return NewClientWithResolver(policy, netResolver{value: net.DefaultResolver})
}

func NewClientWithResolver(policy Policy, resolver Resolver) (*Client, error) {
	normalized, err := policy.normalized()
	if err != nil {
		return nil, err
	}
	if resolver == nil {
		return nil, invalid(ErrInvalidPolicy, "nil_outbound_resolver")
	}
	client := &Client{policy: normalized, resolver: resolver}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if normalized.TLSConfig != nil {
		tlsConfig = normalized.TLSConfig.Clone()
		if tlsConfig.MinVersion == 0 {
			tlsConfig.MinVersion = tls.VersionTLS12
		}
	}
	dialer := &net.Dialer{Timeout: normalized.DialTimeout, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy:                 normalized.Proxy,
		DialContext:           client.dialContext(dialer),
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          normalized.MaxConnsPerHost * 4,
		MaxIdleConnsPerHost:   normalized.MaxConnsPerHost,
		MaxConnsPerHost:       normalized.MaxConnsPerHost,
		IdleConnTimeout:       normalized.IdleConnTimeout,
		TLSHandshakeTimeout:   normalized.TLSHandshakeTimeout,
		ResponseHeaderTimeout: normalized.ResponseHeaderTimeout,
		DisableCompression:    true,
		TLSClientConfig:       tlsConfig,
	}
	client.http = &http.Client{
		Transport:     httpx.RequestMetadataTransport{Base: transport},
		Timeout:       normalized.Timeout,
		CheckRedirect: client.checkRedirect,
	}
	return client, nil
}

func (client *Client) HTTPClient() *http.Client {
	if client == nil {
		return nil
	}
	return client.http
}

func (client *Client) Do(request *http.Request) (*http.Response, error) {
	if client == nil || client.http == nil {
		return nil, invalid(ErrInvalidPolicy, "nil_outbound_client")
	}
	if request == nil || request.URL == nil {
		return nil, reject(ErrURLRejected, "nil_outbound_request", "httpx.outbound.Client.Do", nil)
	}
	if err := client.policy.validateURL(request.URL); err != nil {
		return nil, err
	}
	cloned := request.Clone(request.Context())
	cloned.Header = request.Header.Clone()
	if cloned.Header.Get("User-Agent") == "" {
		cloned.Header.Set("User-Agent", client.policy.UserAgent)
	}
	cloned.Header.Set("Accept-Encoding", "identity")
	response, err := client.http.Do(cloned)
	if err != nil {
		return nil, err
	}
	if err := client.validateResponse(response); err != nil {
		response.Body.Close()
		return nil, err
	}
	return response, nil
}

func (client *Client) checkRedirect(request *http.Request, via []*http.Request) error {
	if len(via) >= client.policy.MaxRedirects {
		return reject(ErrURLRejected, "too_many_outbound_redirects", "httpx.outbound.Client.CheckRedirect", faults.Fields{"redirects": len(via)})
	}
	return client.policy.validateURL(request.URL)
}

func (client *Client) dialContext(dialer *net.Dialer) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, reject(err, "invalid_outbound_dial_address", "httpx.outbound.Client.Dial", faults.Fields{"address": address})
		}
		host = canonicalHost(host)
		// Validate the dial host independently of the URL to defend against
		// redirect and transport rewrites.
		synthetic := &url.URL{Scheme: "https", Host: net.JoinHostPort(host, port)}
		if port == "80" && client.policy.AllowHTTPForTests {
			synthetic.Scheme = "http"
		}
		if err := client.policy.validateURL(synthetic); err != nil {
			return nil, err
		}

		var addresses []netip.Addr
		if literal, parseErr := netip.ParseAddr(host); parseErr == nil {
			addresses = []netip.Addr{literal}
		} else {
			addresses, err = client.resolver.LookupNetIP(ctx, "ip", host)
			if err != nil {
				return nil, faults.Wrap(errors.Join(ErrResolutionFailed, err), faults.CodeUnavailable, "outbound host resolution failed", faults.WithReason("outbound_dns_failed"), faults.WithOperation("httpx.outbound.Client.Dial"), faults.WithField("host", host), faults.WithRetryPolicy(faults.BackoffRetry(0)))
			}
		}
		approved := make([]netip.Addr, 0, len(addresses))
		for _, candidate := range addresses {
			if allowedAddress(candidate, client.policy.AllowPrivateAddresses) {
				approved = append(approved, candidate.Unmap())
			}
		}
		if len(approved) == 0 {
			return nil, reject(ErrAddressNotAllowed, "outbound_address_not_allowed", "httpx.outbound.Client.Dial", faults.Fields{"host": host})
		}
		start := int(client.nextAddress.Add(1)-1) % len(approved)
		var dialErrors []error
		for offset := 0; offset < len(approved); offset++ {
			candidate := approved[(start+offset)%len(approved)]
			connection, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(candidate.String(), port))
			if dialErr == nil {
				return connection, nil
			}
			dialErrors = append(dialErrors, dialErr)
		}
		return nil, faults.Wrap(errors.Join(dialErrors...), faults.CodeUnavailable, "outbound connection failed", faults.WithReason("outbound_dial_failed"), faults.WithOperation("httpx.outbound.Client.Dial"), faults.WithField("host", host), faults.WithRetryPolicy(faults.BackoffRetry(0)))
	}
}

func (client *Client) validateResponse(response *http.Response) error {
	if response == nil || response.Body == nil {
		return faults.New(faults.CodeDataLoss, "outbound server returned an invalid response", faults.WithReason("nil_outbound_response"))
	}
	contentType := response.Header.Get("Content-Type")
	if len(client.policy.AllowedMediaTypes) > 0 {
		mediaType, _, err := mime.ParseMediaType(contentType)
		if err != nil || !contains(client.policy.AllowedMediaTypes, strings.ToLower(mediaType)) {
			return reject(ErrMediaTypeRejected, "outbound_media_type_rejected", "httpx.outbound.Client.ValidateResponse", faults.Fields{"content_type": contentType})
		}
	}
	if response.ContentLength > client.policy.MaxResponseBytes {
		return faults.Wrap(ErrResponseTooLarge, faults.CodeResourceExhausted, "outbound response exceeds configured limit", faults.WithReason("outbound_response_too_large"), faults.WithOperation("httpx.outbound.Client.ValidateResponse"), faults.WithField("content_length", response.ContentLength), faults.WithRetryPolicy(faults.NoRetry()))
	}
	encoding := strings.ToLower(strings.TrimSpace(response.Header.Get("Content-Encoding")))
	var reader io.Reader = response.Body
	closer := io.Closer(response.Body)
	if encoding != "" && encoding != "identity" {
		if encoding != "gzip" || !client.policy.AllowGzip {
			return reject(ErrEncodingRejected, "outbound_encoding_rejected", "httpx.outbound.Client.ValidateResponse", faults.Fields{"content_encoding": encoding})
		}
		gzipReader, err := gzip.NewReader(response.Body)
		if err != nil {
			return faults.Wrap(err, faults.CodeDataLoss, "invalid compressed outbound response", faults.WithReason("invalid_outbound_gzip"))
		}
		compound := &compoundReadCloser{Reader: gzipReader, closers: []io.Closer{gzipReader, response.Body}}
		reader = compound
		closer = compound
		response.Header.Del("Content-Encoding")
		response.ContentLength = -1
	}
	response.Body = &limitedReadCloser{reader: reader, closer: closer, remaining: client.policy.MaxResponseBytes + 1, maximum: client.policy.MaxResponseBytes}
	return nil
}

type limitedReadCloser struct {
	reader    io.Reader
	closer    io.Closer
	remaining int64
	maximum   int64
	exceeded  bool
}

func (value *limitedReadCloser) Read(buffer []byte) (int, error) {
	if value.exceeded {
		return 0, faults.Wrap(ErrResponseTooLarge, faults.CodeResourceExhausted, "outbound response exceeds configured limit", faults.WithReason("outbound_response_too_large"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	if int64(len(buffer)) > value.remaining {
		buffer = buffer[:value.remaining]
	}
	count, err := value.reader.Read(buffer)
	value.remaining -= int64(count)
	if value.remaining <= 0 && err == nil {
		value.exceeded = true
		return count, faults.Wrap(ErrResponseTooLarge, faults.CodeResourceExhausted, "outbound response exceeds configured limit", faults.WithReason("outbound_response_too_large"), faults.WithField("maximum_bytes", value.maximum), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return count, err
}
func (value *limitedReadCloser) Close() error { return value.closer.Close() }

type compoundReadCloser struct {
	io.Reader
	closers []io.Closer
}

func (value *compoundReadCloser) Close() error {
	var result error
	for _, closer := range value.closers {
		result = errors.Join(result, closer.Close())
	}
	return result
}
