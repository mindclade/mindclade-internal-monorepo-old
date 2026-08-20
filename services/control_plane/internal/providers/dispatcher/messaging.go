// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package dispatcher

import (
	mcclock "go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/messaging"
	"go.mindclade.dev/libs/go/servicekit"
	"go.mindclade.dev/services/control_plane/internal/config"
	"go.mindclade.dev/services/control_plane/internal/providers/broker"
)

// newPublisher resolves the configured messaging provider. The provider switch
// and the two gates guarding the in-memory broker live in the shared
// composition root, because the dispatcher is no longer the only publisher.
func newPublisher(settings config.Settings, value mcclock.Clock) (messaging.Publisher, servicekit.Component, error) {
	return broker.NewPublisher(settings, value)
}
