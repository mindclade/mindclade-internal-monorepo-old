// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

package postgres

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"go.mindclade.dev/control/evidence"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
)

var _ evidence.Repository = (*Store)(nil)

func (store *Store) AppendClaim(ctx context.Context, claim evidence.Claim, verification evidence.Verification) error {
	const operation = "registry.postgres.AppendClaim"
	if err := store.validate(ctx, operation); err != nil {
		return err
	}
	if err := claim.Validate(); err != nil {
		return invalidEvidence(ctx, err, operation, "evidence_claim_invalid")
	}
	if err := verification.Validate(); err != nil {
		return invalidEvidence(ctx, err, operation, "evidence_verification_invalid")
	}
	if !verification.ClaimDigest.Equal(claim.ClaimDigest) {
		return invalidEvidence(ctx, ErrEvidenceImmutable, operation, "evidence_claim_verification_mismatch")
	}
	claimDocument, err := json.Marshal(claim)
	if err != nil {
		return internal(ctx, err, operation, "evidence_claim_encoding_failed")
	}
	verificationDocument, err := json.Marshal(verification)
	if err != nil {
		return internal(ctx, err, operation, "evidence_verification_encoding_failed")
	}
	now := store.clock.Now().Round(0).UTC()
	query := fmt.Sprintf(`WITH claim_write AS (
INSERT INTO %s (claim_digest, subject_digest, control_id, owner, valid_until, document, written_at)
VALUES ($1,$2,$3,$4,$5,$6,$7)
ON CONFLICT (claim_digest) DO NOTHING
RETURNING claim_digest
), claim_identity AS (
SELECT claim_digest FROM claim_write
UNION ALL
SELECT claim_digest FROM %s WHERE claim_digest=$1
LIMIT 1
)
INSERT INTO %s (verification_digest, claim_digest, policy_digest, policy_epoch, result, verified_at, valid_until, document, written_at)
SELECT $8,claim_digest,$9,$10,$11,$12,$13,$14,$7 FROM claim_identity
ON CONFLICT (verification_digest) DO NOTHING`, store.claims, store.claims, store.verifications)
	if _, err := store.executor(ctx).ExecContext(ctx, query,
		claim.ClaimDigest.String(), claim.SubjectDigest.String(), claim.ControlID, claim.Owner, claim.ValidUntil,
		claimDocument, now, verification.VerificationDigest.String(), verification.PolicyDigest.String(), int64(verification.PolicyEpoch),
		verification.Result, verification.VerifiedAt, verification.ValidUntil, verificationDocument,
	); err != nil {
		return provider(ctx, err, operation)
	}
	return nil
}

func (store *Store) Current(ctx context.Context, subject identifiers.Digest, control string) (evidence.Claim, evidence.Verification, error) {
	const operation = "registry.postgres.CurrentEvidence"
	if err := store.validate(ctx, operation); err != nil {
		return evidence.Claim{}, evidence.Verification{}, err
	}
	if !subject.Valid() || control == "" {
		return evidence.Claim{}, evidence.Verification{}, invalidEvidence(ctx, evidence.ErrNotFound, operation, "evidence_lookup_invalid")
	}
	query := fmt.Sprintf(`SELECT c.document, v.document
FROM %s c JOIN %s v ON v.claim_digest=c.claim_digest
WHERE c.subject_digest=$1 AND c.control_id=$2
ORDER BY v.verified_at DESC, v.verification_digest DESC
LIMIT 1`, store.claims, store.verifications)
	var claimDocument, verificationDocument []byte
	err := store.executor(ctx).QueryRowContext(ctx, query, subject.String(), control).Scan(&claimDocument, &verificationDocument)
	if errors.Is(err, sql.ErrNoRows) {
		return evidence.Claim{}, evidence.Verification{}, evidenceMissing(ctx, operation, subject, control)
	}
	if err != nil {
		return evidence.Claim{}, evidence.Verification{}, provider(ctx, err, operation)
	}
	var claim evidence.Claim
	if err := json.Unmarshal(claimDocument, &claim); err != nil {
		return evidence.Claim{}, evidence.Verification{}, internal(ctx, err, operation, "evidence_claim_decoding_failed")
	}
	var verification evidence.Verification
	if err := json.Unmarshal(verificationDocument, &verification); err != nil {
		return evidence.Claim{}, evidence.Verification{}, internal(ctx, err, operation, "evidence_verification_decoding_failed")
	}
	if err := claim.Validate(); err != nil {
		return evidence.Claim{}, evidence.Verification{}, internal(ctx, err, operation, "stored_evidence_claim_invalid")
	}
	if err := verification.Validate(); err != nil {
		return evidence.Claim{}, evidence.Verification{}, internal(ctx, err, operation, "stored_evidence_verification_invalid")
	}
	return claim, verification, nil
}

func (store *Store) Evidence(ctx context.Context, subject identifiers.Digest) ([]evidence.Record, error) {
	const operation = "registry.postgres.Evidence"
	if err := store.validate(ctx, operation); err != nil {
		return nil, err
	}
	if !subject.Valid() {
		return nil, invalidEvidence(ctx, evidence.ErrNotFound, operation, "evidence_lookup_invalid")
	}
	query := fmt.Sprintf(`SELECT c.document, v.document
FROM %s c JOIN %s v ON v.claim_digest=c.claim_digest
WHERE c.subject_digest=$1
ORDER BY c.control_id, v.verified_at DESC, v.verification_digest DESC`, store.claims, store.verifications)
	rows, err := store.executor(ctx).QueryContext(ctx, query, subject.String())
	if err != nil {
		return nil, provider(ctx, err, operation)
	}
	defer rows.Close()
	records := make([]evidence.Record, 0)
	for rows.Next() {
		var claimDocument, verificationDocument []byte
		if err := rows.Scan(&claimDocument, &verificationDocument); err != nil {
			return nil, provider(ctx, err, operation)
		}
		var record evidence.Record
		if err := json.Unmarshal(claimDocument, &record.Claim); err != nil {
			return nil, internal(ctx, err, operation, "evidence_claim_decoding_failed")
		}
		if err := json.Unmarshal(verificationDocument, &record.Verification); err != nil {
			return nil, internal(ctx, err, operation, "evidence_verification_decoding_failed")
		}
		if err := record.Claim.Validate(); err != nil {
			return nil, internal(ctx, err, operation, "stored_evidence_claim_invalid")
		}
		if err := record.Verification.Validate(); err != nil {
			return nil, internal(ctx, err, operation, "stored_evidence_verification_invalid")
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, provider(ctx, err, operation)
	}
	if len(records) == 0 {
		return nil, evidenceMissing(ctx, operation, subject, "")
	}
	return records, nil
}

func (store *Store) PutDecision(ctx context.Context, signed evidence.SignedDecision) error {
	const operation = "registry.postgres.PutEligibilityDecision"
	if err := store.validate(ctx, operation); err != nil {
		return err
	}
	if err := signed.Validate(); err != nil {
		return invalidEvidence(ctx, err, operation, "eligibility_decision_invalid")
	}
	document, err := json.Marshal(signed)
	if err != nil {
		return internal(ctx, err, operation, "eligibility_decision_encoding_failed")
	}
	decision := signed.Decision
	query := fmt.Sprintf(`INSERT INTO %s (
decision_digest, bundle_digest, policy_digest, policy_epoch, result, evaluated_at, expires_at, document, written_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
ON CONFLICT (decision_digest) DO NOTHING`, store.decisions)
	result, err := store.executor(ctx).ExecContext(ctx, query,
		decision.DecisionDigest.String(), decision.BundleDigest.String(), decision.PolicyDigest.String(), int64(decision.PolicyEpoch),
		decision.Result, decision.EvaluatedAt, decision.ExpiresAt, document, store.clock.Now().Round(0).UTC(),
	)
	if err != nil {
		return provider(ctx, err, operation)
	}
	if affected, rowsErr := result.RowsAffected(); rowsErr == nil && affected == 1 {
		return nil
	}
	var stored []byte
	if err := store.executor(ctx).QueryRowContext(ctx,
		fmt.Sprintf(`SELECT document FROM %s WHERE decision_digest=$1`, store.decisions),
		decision.DecisionDigest.String(),
	).Scan(&stored); err != nil {
		return provider(ctx, err, operation)
	}
	if bytes.Equal(stored, document) {
		return nil
	}
	return immutableEvidence(ctx, operation, decision.DecisionDigest)
}

func (store *Store) Decision(ctx context.Context, digest identifiers.Digest) (evidence.SignedDecision, bool, error) {
	const operation = "registry.postgres.EligibilityDecision"
	if err := store.validate(ctx, operation); err != nil {
		return evidence.SignedDecision{}, false, err
	}
	if !digest.Valid() {
		return evidence.SignedDecision{}, false, invalidEvidence(ctx, evidence.ErrNotFound, operation, "eligibility_digest_invalid")
	}
	query := fmt.Sprintf(`SELECT d.document, (r.decision_digest IS NOT NULL)
FROM %s d LEFT JOIN %s r ON r.decision_digest=d.decision_digest
WHERE d.decision_digest=$1`, store.decisions, store.revocations)
	var document []byte
	var revoked bool
	err := store.executor(ctx).QueryRowContext(ctx, query, digest.String()).Scan(&document, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return evidence.SignedDecision{}, false, decisionMissing(ctx, operation, digest)
	}
	if err != nil {
		return evidence.SignedDecision{}, false, provider(ctx, err, operation)
	}
	var signed evidence.SignedDecision
	if err := json.Unmarshal(document, &signed); err != nil {
		return evidence.SignedDecision{}, false, internal(ctx, err, operation, "eligibility_decision_decoding_failed")
	}
	if err := signed.Validate(); err != nil {
		return evidence.SignedDecision{}, false, internal(ctx, err, operation, "stored_eligibility_decision_invalid")
	}
	return signed, revoked, nil
}

func (store *Store) Revoke(ctx context.Context, digest identifiers.Digest, reason string, revokedAt time.Time) error {
	const operation = "registry.postgres.RevokeEligibilityDecision"
	if err := store.validate(ctx, operation); err != nil {
		return err
	}
	if !digest.Valid() || reason == "" || revokedAt.IsZero() {
		return invalidEvidence(ctx, evidence.ErrNotFound, operation, "eligibility_revocation_invalid")
	}
	query := fmt.Sprintf(`INSERT INTO %s (decision_digest, reason, revoked_at, written_at)
SELECT decision_digest, $2, $3, $4 FROM %s WHERE decision_digest=$1
ON CONFLICT (decision_digest) DO NOTHING`, store.revocations, store.decisions)
	result, err := store.executor(ctx).ExecContext(ctx, query, digest.String(), reason, revokedAt.Round(0).UTC(), store.clock.Now().Round(0).UTC())
	if err != nil {
		return provider(ctx, err, operation)
	}
	if affected, rowsErr := result.RowsAffected(); rowsErr == nil && affected == 1 {
		return nil
	}
	var storedReason string
	err = store.executor(ctx).QueryRowContext(ctx,
		fmt.Sprintf(`SELECT reason FROM %s WHERE decision_digest=$1`, store.revocations), digest.String(),
	).Scan(&storedReason)
	if errors.Is(err, sql.ErrNoRows) {
		return decisionMissing(ctx, operation, digest)
	}
	if err != nil {
		return provider(ctx, err, operation)
	}
	if storedReason == reason {
		return nil
	}
	return immutableEvidence(ctx, operation, digest)
}

func invalidEvidence(ctx context.Context, err error, operation, reason string) error {
	return faults.Wrap(err, faults.CodeInvalidArgument, "evidence ledger record is invalid",
		faults.WithReason(reason), faults.WithOperation(operation), faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
}

func evidenceMissing(ctx context.Context, operation string, subject identifiers.Digest, control string) error {
	return faults.Wrap(evidence.ErrNotFound, faults.CodeNotFound, "evidence claim was not found",
		faults.WithReason("evidence_claim_missing"), faults.WithOperation(operation), faults.WithField("subject_digest", subject.String()),
		faults.WithField("control_id", control), faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
}

func decisionMissing(ctx context.Context, operation string, digest identifiers.Digest) error {
	return faults.Wrap(evidence.ErrNotFound, faults.CodeNotFound, "eligibility decision was not found",
		faults.WithReason("eligibility_decision_missing"), faults.WithOperation(operation), faults.WithField("decision_digest", digest.String()),
		faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
}

func immutableEvidence(ctx context.Context, operation string, digest identifiers.Digest) error {
	return faults.Wrap(ErrEvidenceImmutable, faults.CodeFailedPrecondition, "evidence ledger record is immutable",
		faults.WithReason("evidence_record_immutable"), faults.WithOperation(operation), faults.WithField("digest", digest.String()),
		faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
}
