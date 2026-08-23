// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package providers

import (
	mcclock "go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/observability"
	"go.mindclade.dev/libs/go/pagination"
	"go.mindclade.dev/libs/go/retry"
	"go.mindclade.dev/libs/go/signing"
	"go.mindclade.dev/services/control_plane/internal/config"
)

// Mechanisms are the provider-independent foundation objects shared by every
// control-plane role. They depend on configuration only, never on a reachable
// external system, so construction failures are always deployment errors.
type Mechanisms struct {
	Clock         mcclock.Clock
	IDs           *identifiers.Generator
	Observability *observability.Runtime
	Retry         *retry.Executor
	Signer        signing.Signer
	Verifier      signing.Verifier
	Pagination    *pagination.Codec
}

func NewMechanisms(settings config.Settings) (Mechanisms, error) {
	value := mcclock.RealClock{}
	ids, err := identifiers.NewGenerator()
	if err != nil {
		return Mechanisms{}, err
	}
	resource, err := observability.NewResource(
		settings.ServiceName,
		observability.WithServiceNamespace("mindclade"),
		observability.WithDeploymentEnvironment(string(settings.Environment)),
	)
	if err != nil {
		return Mechanisms{}, err
	}
	telemetry, err := observability.NewRuntime(resource, observability.WithClock(value))
	if err != nil {
		return Mechanisms{}, err
	}
	executor, err := retry.NewExecutor(retry.DefaultPolicy(), retry.WithClock(value))
	if err != nil {
		return Mechanisms{}, err
	}
	signer, verifier, err := newSigning(settings, value)
	if err != nil {
		return Mechanisms{}, err
	}
	codec, err := pagination.NewCodec(signer, verifier, pagination.CursorDomain, value, settings.PaginationTTL)
	if err != nil {
		return Mechanisms{}, err
	}
	return Mechanisms{
		Clock:         value,
		IDs:           ids,
		Observability: telemetry,
		Retry:         executor,
		Signer:        signer,
		Verifier:      verifier,
		Pagination:    codec,
	}, nil
}

// newSigning builds the symmetric signer and the single-key verification set
// used for pagination cursors and internal tickets. Key rotation adds entries
// to the verification set; the signer always uses the active key.
//
// One key deliberately serves several purposes, so nothing here keeps a
// signature minted for one of them from being replayed as another. That
// separation lives in the signed bytes instead: every claim document commits to
// its type through the canonical encoders in control/, and page tokens commit
// to pagination.CursorDomain. Any future consumer of this signer must commit to
// its own domain the same way before it signs anything.
func newSigning(settings config.Settings, value mcclock.Clock) (signing.Signer, signing.Verifier, error) {
	keyID, err := signing.ParseKeyID(settings.SigningKeyID)
	if err != nil {
		return nil, nil, err
	}
	key := []byte(settings.SigningHMACKey)
	if len(key) < signing.MinimumHMACKeySize {
		return nil, nil, faults.New(
			faults.CodeFailedPrecondition,
			"control-plane signing key is not configured",
			faults.WithReason("signing_key_not_configured"),
			faults.WithOperation("controlplane.providers.newSigning"),
			faults.WithField("minimum_key_bytes", signing.MinimumHMACKeySize),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	signer, err := signing.NewHMACSigner(keyID, key)
	if err != nil {
		return nil, nil, err
	}
	keys, err := signing.NewKeySet(value, signing.VerificationKey{
		ID:        keyID,
		Algorithm: signing.AlgorithmHMACSHA256,
		HMACKey:   key,
	})
	if err != nil {
		return nil, nil, err
	}
	return signer, keys, nil
}
