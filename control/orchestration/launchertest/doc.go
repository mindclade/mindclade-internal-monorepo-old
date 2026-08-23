// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package launchertest is the shared conformance suite every orchestration
// launcher must pass.
//
// It exists because Launcher is the seam that lets one workflow run against
// Kubernetes, Slurm, or a developer laptop. That promise is only worth
// something if the three backends agree on the awkward cases, and the awkward
// cases are exactly the ones each adapter's own tests are least likely to
// invent independently: a duplicate queue delivery, a cancellation that raced
// a completion, an envelope whose ticket already expired, a message from a
// replaced replica carrying an old fence.
//
// The suite asserts the contract in control/orchestration/executor.go and
// nothing beyond it. It never asserts a particular AttemptState, because the
// state a backend reports at a given moment is a property of that backend --
// the local runner blocks until the process exits and reports a terminal
// state, while a JobSet is suspended for as long as Kueue withholds quota.
// What it does assert is that the reported states are valid, do not regress
// once terminal, and stay attached to one external identity.
package launchertest
