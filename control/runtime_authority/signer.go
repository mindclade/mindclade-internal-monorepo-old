// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package runtime_authority

import "mindclade.internal/libs/go/signing"

type ExecutionTicket struct {
	Claims    ExecutionTicketClaims
	Signature signing.Signature
}
type AdmissionGrant struct {
	Claims    AdmissionGrantClaims
	Signature signing.Signature
}
type RouteSnapshot struct {
	Claims    RouteSnapshotClaims
	Signature signing.Signature
}
type RevocationSnapshot struct {
	Claims    RevocationSnapshotClaims
	Signature signing.Signature
}
