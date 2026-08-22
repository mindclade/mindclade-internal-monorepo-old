// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"strings"

	"go.mindclade.dev/control/evidence"
	"go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/services/control_plane/internal/config"
	"go.mindclade.dev/services/control_plane/internal/providers/kms"
	registrystore "go.mindclade.dev/services/control_plane/internal/store/postgres"
)

const maximumEligibilityPolicyBytes = 1 << 20

func newEligibilityEngine(ctx context.Context, settings config.Settings, db *sql.DB, value clock.Clock) (eligibilityEngine, error) {
	policyPath := strings.TrimSpace(settings.EligibilityPolicyPath)
	keyVersion := strings.TrimSpace(settings.EligibilityKMSKeyVersion)
	if policyPath == "" && keyVersion == "" {
		if settings.Environment == config.EnvironmentProduction || settings.Environment == config.EnvironmentStaging {
			return nil, eligibilityHTTPError(faults.CodeFailedPrecondition, "eligibility_configuration_required", "production eligibility configuration is required")
		}
		return nil, nil
	}
	if ctx == nil || db == nil || value == nil {
		return nil, eligibilityHTTPError(faults.CodeFailedPrecondition, "eligibility_runtime_unconfigured", "production eligibility runtime is not configured")
	}
	document, err := os.ReadFile(policyPath)
	if err != nil {
		return nil, faults.Wrap(err, faults.CodeFailedPrecondition, "production eligibility policy cannot be read",
			faults.WithReason("eligibility_policy_unreadable"), faults.WithOperation("controlplane.api.newEligibilityEngine"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	if len(document) == 0 || len(document) > maximumEligibilityPolicyBytes {
		return nil, eligibilityHTTPError(faults.CodeFailedPrecondition, "eligibility_policy_size_invalid", "production eligibility policy size is invalid")
	}
	var policy evidence.Policy
	if err := json.Unmarshal(document, &policy); err != nil {
		return nil, faults.Wrap(err, faults.CodeInvalidArgument, "production eligibility policy is invalid",
			faults.WithReason("eligibility_policy_json_invalid"), faults.WithOperation("controlplane.api.newEligibilityEngine"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	if !policy.ValidUntil.After(value.Now().Round(0).UTC()) {
		return nil, eligibilityHTTPError(faults.CodeFailedPrecondition, "eligibility_policy_expired", "production eligibility policy is expired")
	}
	signer, err := kms.NewEd25519Signer(ctx, settings.EligibilitySigningKeyID, keyVersion)
	if err != nil {
		return nil, err
	}
	repository, err := registrystore.New(db, registrystore.WithClock(value))
	if err != nil {
		return nil, err
	}
	return evidence.Service{Repository: repository, Policy: policy, Signer: signer, Clock: value}, nil
}
