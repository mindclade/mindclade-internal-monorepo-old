// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package runtime_authority

import (
	"bytes"
	"mindclade.internal/libs/go/identifiers"
	"testing"
	"time"
)

func TestCanonicalTicketDeterministic(t *testing.T) {
	now := time.UnixMilli(1800000000000).UTC()
	c := ExecutionTicketClaims{TicketID: "ticket_019c0000000070008000000000000001", Issuer: "control", TenantID: "tenant_019c0000000070008000000000000002", WorkspaceID: "workspace_019c0000000070008000000000000003", StageID: "stage_019c0000000070008000000000000004", Attempt: 1, FencingToken: 2, ResolvedConfigDigest: identifiers.SHA256([]byte("config")), Artifacts: ArtifactGrant{}, Budget: ExecutionBudget{CPUMillis: 1, ResidentMemoryBytes: 1, OpenFileDescriptors: 1, CPUWorkerThreads: 1}, ExecutionClass: "cpu", NotBefore: now, Deadline: now.Add(time.Minute), Expires: now.Add(time.Second), PolicyEpoch: 1, RouteSnapshotVersion: 1, RevocationEpoch: 1}
	a, e := c.CanonicalBytes()
	if e != nil {
		t.Fatal(e)
	}
	b, e := c.CanonicalBytes()
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("canonical claims changed")
	}
}
