// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package routing

import (
	"context"
	"go.mindclade.dev/control/runtime_authority"
)

type Publisher interface {
	PublishRouteSnapshot(context.Context, string, runtime_authority.RouteSnapshot) error
}
