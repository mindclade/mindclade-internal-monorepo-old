// Copyright 2026 Mindclade. All rights reserved.
package routing

import (
	"context"
	"mindclade.internal/control/runtime_authority"
)

type Publisher interface {
	PublishRouteSnapshot(context.Context, string, runtime_authority.RouteSnapshot) error
}
