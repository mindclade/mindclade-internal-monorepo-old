// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package routing

import (
	"context"
	"mindclade.internal/control/runtime_authority"
	"mindclade.internal/libs/go/identifiers"
	"mindclade.internal/libs/go/signing"
	"testing"
	"time"
)

func TestSnapshotPublicationMonotonic(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	sg, _ := signing.NewHMACSigner(signing.MustParseKeyID("route"), key)
	svc := Service{Repository: NewMemoryRepository(), Builder: SnapshotBuilder{Issuer: runtime_authority.Issuer{Name: "control", Signer: sg}, TTL: time.Minute}}
	depID, _ := identifiers.NewID(identifiers.MustParseKind("deployment"))
	r := Deployment{DeploymentID: depID.String(), ModelBundleDigest: identifiers.SHA256([]byte("m")), EngineBundleDigest: identifiers.SHA256([]byte("e")), Endpoint: "unix:///runtime", Region: "us", Weight: 1, LeaseExpires: time.Now().Add(time.Hour), Capabilities: []string{"fold"}}
	p := Policy{PolicyEpoch: 1, RevocationEpoch: 1, MinimumRuntimeVersion: "1", PolicyDigest: identifiers.SHA256([]byte("p"))}
	if _, err := svc.PublishAt(context.Background(), "us", 1, p, []Deployment{r}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PublishAt(context.Background(), "us", 1, p, []Deployment{r}, time.Now()); err == nil {
		t.Fatal("expected non-monotonic version rejection")
	}
}
