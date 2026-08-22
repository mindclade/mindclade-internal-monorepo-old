// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

package kms

import (
	"context"
	"encoding/base64"
	"hash/crc32"
	"strings"

	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/signing"
	cloudkms "google.golang.org/api/cloudkms/v1"
)

type Ed25519Signer struct {
	keyID      signing.KeyID
	keyVersion string
	service    *cloudkms.Service
}

func NewEd25519Signer(ctx context.Context, keyID, keyVersion string) (*Ed25519Signer, error) {
	if ctx == nil {
		return nil, invalid("kms_context_nil", "Cloud KMS signer context is required", nil)
	}
	parsedKeyID, err := signing.ParseKeyID(strings.TrimSpace(keyID))
	if err != nil {
		return nil, err
	}
	keyVersion = strings.TrimSpace(keyVersion)
	if keyVersion == "" || len(keyVersion) > 1024 || !strings.Contains(keyVersion, "/cryptoKeyVersions/") {
		return nil, invalid("kms_key_version_invalid", "Cloud KMS key version is invalid", nil)
	}
	service, err := cloudkms.NewService(ctx)
	if err != nil {
		return nil, unavailable("kms_client_unavailable", "Cloud KMS client is unavailable", err)
	}
	return &Ed25519Signer{keyID: parsedKeyID, keyVersion: keyVersion, service: service}, nil
}

func (signer *Ed25519Signer) Sign(ctx context.Context, payload []byte) (signing.Signature, error) {
	if ctx == nil || signer == nil || signer.service == nil || !signer.keyID.Valid() || signer.keyVersion == "" {
		return signing.Signature{}, invalid("kms_signer_unconfigured", "Cloud KMS signer is not configured", nil)
	}
	if len(payload) == 0 {
		return signing.Signature{}, invalid("kms_payload_empty", "Cloud KMS signing payload is empty", nil)
	}
	checksum := crc32.Checksum(payload, crc32.MakeTable(crc32.Castagnoli))
	request := &cloudkms.AsymmetricSignRequest{
		Data:       base64.StdEncoding.EncodeToString(payload),
		DataCrc32c: int64(checksum),
	}
	response, err := signer.service.Projects.Locations.KeyRings.CryptoKeys.CryptoKeyVersions.
		AsymmetricSign(signer.keyVersion, request).Context(ctx).Do()
	if err != nil {
		return signing.Signature{}, unavailable("kms_sign_failed", "Cloud KMS signing failed", err)
	}
	if response.Name != signer.keyVersion || !response.VerifiedDataCrc32c {
		return signing.Signature{}, unavailable("kms_integrity_unverified", "Cloud KMS did not verify signing request integrity", nil)
	}
	value, err := base64.StdEncoding.DecodeString(response.Signature)
	if err != nil {
		return signing.Signature{}, unavailable("kms_signature_encoding_invalid", "Cloud KMS returned an invalid signature", err)
	}
	if len(value) != 64 || int64(crc32.Checksum(value, crc32.MakeTable(crc32.Castagnoli))) != response.SignatureCrc32c {
		return signing.Signature{}, unavailable("kms_signature_integrity_invalid", "Cloud KMS signature integrity check failed", nil)
	}
	return signing.NewSignature(signing.AlgorithmEd25519, signer.keyID, value)
}

func invalid(reason, message string, cause error) error {
	if cause == nil {
		return faults.New(faults.CodeInvalidArgument, message, faults.WithReason(reason), faults.WithOperation("controlplane.kms"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return faults.Wrap(cause, faults.CodeInvalidArgument, message, faults.WithReason(reason), faults.WithOperation("controlplane.kms"), faults.WithRetryPolicy(faults.NoRetry()))
}

func unavailable(reason, message string, cause error) error {
	if cause == nil {
		return faults.New(faults.CodeUnavailable, message, faults.WithReason(reason), faults.WithOperation("controlplane.kms"), faults.WithRetryPolicy(faults.BackoffRetry(3)))
	}
	return faults.Wrap(cause, faults.CodeUnavailable, message, faults.WithReason(reason), faults.WithOperation("controlplane.kms"), faults.WithRetryPolicy(faults.BackoffRetry(3)))
}
