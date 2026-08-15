// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package pagination

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	mcclock "mindclade.internal/libs/go/clock"
	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/signing"
)

type Codec struct {
	signer   signing.Signer
	verifier signing.Verifier
	clock    mcclock.Clock
	ttl      time.Duration
}

func NewCodec(signer signing.Signer, verifier signing.Verifier, clock mcclock.Clock, ttl time.Duration) (*Codec, error) {
	if signer == nil || verifier == nil || ttl <= 0 {
		return nil, invalid(ErrInvalidRequest, "invalid_cursor_codec", "pagination.NewCodec")
	}
	if clock == nil {
		clock = mcclock.RealClock{}
	}
	return &Codec{signer: signer, verifier: verifier, clock: clock, ttl: ttl}, nil
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
	signature, err := codec.signer.Sign(ctx, payload)
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
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return Cursor{}, invalid(ErrInvalidCursor, "invalid_cursor_token", "pagination.Codec.Decode")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Cursor{}, invalid(ErrInvalidCursor, "invalid_cursor_payload_encoding", "pagination.Codec.Decode")
	}
	signatureText, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Cursor{}, invalid(ErrInvalidCursor, "invalid_cursor_signature_encoding", "pagination.Codec.Decode")
	}
	var signature signing.Signature
	if err := signature.UnmarshalText(signatureText); err != nil {
		return Cursor{}, err
	}
	if err := codec.verifier.Verify(ctx, payload, signature); err != nil {
		return Cursor{}, err
	}
	var cursor Cursor
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
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
