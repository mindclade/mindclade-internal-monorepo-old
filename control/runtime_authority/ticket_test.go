// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package runtime_authority

import (
	"context"
	"mindclade.internal/libs/go/clock"
	"mindclade.internal/libs/go/identifiers"
	"mindclade.internal/libs/go/signing"
	"testing"
	"time"
)

func testID(t *testing.T, kind string) string {
	t.Helper()
	id, err := identifiers.NewID(identifiers.MustParseKind(kind))
	if err != nil {
		t.Fatal(err)
	}
	return id.String()
}
func TestExecutionTicketIssueVerifyAndFence(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	fc := clock.NewFake(now)
	key := []byte("0123456789abcdef0123456789abcdef")
	kid := signing.MustParseKeyID("runtime-test")
	sg, _ := signing.NewHMACSigner(kid, key)
	ks, _ := signing.NewKeySet(fc, signing.VerificationKey{ID: kid, Algorithm: signing.AlgorithmHMACSHA256, HMACKey: key})
	claims := ExecutionTicketClaims{TicketID: testID(t, "ticket"), TenantID: testID(t, "tenant"), WorkspaceID: testID(t, "workspace"), StageID: testID(t, "stage"), Attempt: 1, FencingToken: 7, ResolvedConfigDigest: identifiers.SHA256([]byte("config")), Artifacts: ArtifactGrant{ReadableDigests: []string{"sha256:a"}, MaximumReadBytes: 1}, Budget: ExecutionBudget{CPUMillis: 1000, ResidentMemoryBytes: 1024, OpenFileDescriptors: 16, CPUWorkerThreads: 1}, ExecutionClass: "cpu", NotBefore: now.Add(-time.Second), Deadline: now.Add(time.Minute), Expires: now.Add(30 * time.Second), PolicyEpoch: 3, RouteSnapshotVersion: 5, RevocationEpoch: 2}
	ticket, err := (Issuer{Name: "control-plane", Signer: sg, Clock: fc}).IssueExecutionTicket(context.Background(), claims)
	if err != nil {
		t.Fatal(err)
	}
	v := ValidationContext{Clock: fc, Verifier: ks, ExpectedIssuer: "control-plane", MinimumPolicyEpoch: 3, MinimumRouteVersion: 5, Fencing: StaticFencingFloor{claims.StageID: 7}, Revocations: SnapshotRevocations{Claims: RevocationSnapshotClaims{Epoch: 2, Created: now.Add(-time.Minute), Expires: now.Add(time.Hour)}}}
	if err := v.ValidateExecutionTicket(context.Background(), ticket); err != nil {
		t.Fatal(err)
	}
	v.Fencing = StaticFencingFloor{claims.StageID: 8}
	if err := v.ValidateExecutionTicket(context.Background(), ticket); err == nil {
		t.Fatal("expected stale fence rejection")
	}
}
