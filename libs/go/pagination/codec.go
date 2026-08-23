// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package pagination

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	mcclock "go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/signing"
)

// CursorDomain is the purpose committed to by every page-token signature.
//
// It is versioned because the signed bytes are a format, not just a value: if
// the cursor encoding ever changes incompatibly, minting under a new domain
// retires every old token by construction instead of leaving two encodings
// mutually verifiable under one key.
const CursorDomain = signing.Domain("pagination-cursor/v1")

type Codec struct {
	signer   signing.Signer
	verifier signing.Verifier
	domain   signing.Domain
	clock    mcclock.Clock
	ttl      time.Duration
}

// NewCodec requires a domain. It is a parameter rather than a package constant
// because the control plane signs page tokens with the same key it uses for
// execution tickets, admission grants, route snapshots, revocation snapshots,
// and evidence claims: without a purpose in the signed bytes, a signature this
// codec mints over attacker-influenced cursor values is a signature, full stop,
// and is structurally valid anywhere else that key is trusted. Requiring the
// caller to name the purpose is what makes forgetting it a compile error rather
// than a silent downgrade to no separation at all.
//
// Pass CursorDomain unless you are deliberately minting a distinct token class.
func NewCodec(signer signing.Signer, verifier signing.Verifier, domain signing.Domain, clock mcclock.Clock, ttl time.Duration) (*Codec, error) {
	if signer == nil || verifier == nil || ttl <= 0 || !domain.Valid() {
		return nil, invalid(ErrInvalidRequest, "invalid_cursor_codec", "pagination.NewCodec")
	}
	if clock == nil {
		clock = mcclock.RealClock{}
	}
	return &Codec{signer: signer, verifier: verifier, domain: domain, clock: clock, ttl: ttl}, nil
}
func (codec *Codec) Encode(ctx context.Context, binding Binding, values ...string) (string, error) {
	if codec == nil {
		return "", invalid(ErrInvalidRequest, "nil_cursor_codec", "pagination.Codec.Encode")
	}
	now := codec.clock.Now().Round(0).UTC()
	cursor, err := NewCursor(binding, values, now, now.Add(codec.ttl))
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", faults.Wrap(err, faults.CodeInternal, "pagination cursor encoding failed", faults.WithReason("cursor_encoding_failed"), faults.WithOperation("pagination.Codec.Encode"))
	}
	// The token carries the bare payload; only the signed bytes are domain
	// separated. Keeping the domain out of the wire format means a token cannot
	// assert its own purpose — the verifier supplies it, so a cursor cannot
	// claim to be a ticket by rewriting a field an attacker controls.
	preimage, err := signing.Preimage(codec.domain, payload)
	if err != nil {
		return "", err
	}
	signature, err := codec.signer.Sign(ctx, preimage)
	if err != nil {
		return "", err
	}
	signatureText, err := signature.MarshalText()
	if err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(signatureText)
	if len(token) > MaximumTokenBytes {
		return "", invalid(ErrInvalidCursor, "encoded_cursor_too_large", "pagination.Codec.Encode")
	}
	return token, nil
}
func (codec *Codec) Decode(ctx context.Context, token string, expected Binding) (Cursor, error) {
	if codec == nil || token == "" || len(token) > MaximumTokenBytes {
		return Cursor{}, invalid(ErrInvalidCursor, "invalid_cursor_token", "pagination.Codec.Decode")
	}
	if err := expected.Validate(); err != nil {
		return Cursor{}, err
	}
	payloadPart, signaturePart, found := strings.Cut(token, ".")
	if !found || strings.Contains(signaturePart, ".") {
		return Cursor{}, invalid(ErrInvalidCursor, "invalid_cursor_token", "pagination.Codec.Decode")
	}
	payload, err := base64.RawURLEncoding.DecodeString(payloadPart)
	if err != nil {
		return Cursor{}, invalid(ErrInvalidCursor, "invalid_cursor_payload_encoding", "pagination.Codec.Decode")
	}
	signatureText, err := base64.RawURLEncoding.DecodeString(signaturePart)
	if err != nil {
		return Cursor{}, invalid(ErrInvalidCursor, "invalid_cursor_signature_encoding", "pagination.Codec.Decode")
	}
	var signature signing.Signature
	if err := signature.UnmarshalText(signatureText); err != nil {
		return Cursor{}, err
	}
	preimage, err := signing.Preimage(codec.domain, payload)
	if err != nil {
		return Cursor{}, err
	}
	if err := codec.verifier.Verify(ctx, preimage, signature); err != nil {
		return Cursor{}, err
	}
	var cursor Cursor
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cursor); err != nil {
		return Cursor{}, invalid(ErrInvalidCursor, "invalid_cursor_payload", "pagination.Codec.Decode")
	}
	if err := cursor.Validate(); err != nil {
		return Cursor{}, err
	}
	now := codec.clock.Now().UTC()
	if !now.Before(cursor.ExpiresAt) {
		return Cursor{}, faults.Wrap(ErrExpiredCursor, faults.CodeInvalidArgument, "page token has expired", faults.WithReason("page_token_expired"), faults.WithOperation("pagination.Codec.Decode"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	if cursor.IssuedAt.After(now.Add(time.Minute)) {
		return Cursor{}, invalid(ErrInvalidCursor, "cursor_issued_in_future", "pagination.Codec.Decode")
	}
	if !cursor.Matches(expected) {
		return Cursor{}, faults.Wrap(ErrCursorMismatch, faults.CodeInvalidArgument, "page token does not match the query", faults.WithReason("page_token_binding_mismatch"), faults.WithOperation("pagination.Codec.Decode"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	cursor.Order = append([]Order(nil), cursor.Order...)
	cursor.Values = append([]string(nil), cursor.Values...)
	return cursor, nil
}
