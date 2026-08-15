// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package outbox

import coordination "mindclade.internal/libs/go/coordination/outbox"

// Status is the durable publication state of an outbox record.
type Status = coordination.State

// State is retained for callers using the coordination vocabulary.
type State = coordination.State

const (
	StatusPending    Status = coordination.StatePending
	StatusClaimed    Status = coordination.StateClaimed
	StatusPublished  Status = coordination.StatePublished
	StatusDeadLetter Status = coordination.StateDeadLetter

	StatePending    State = coordination.StatePending
	StateClaimed    State = coordination.StateClaimed
	StatePublished  State = coordination.StatePublished
	StateDeadLetter State = coordination.StateDeadLetter
)
