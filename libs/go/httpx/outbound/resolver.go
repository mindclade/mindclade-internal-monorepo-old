// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package outbound

import (
	"context"
	"net"
	"net/netip"
)

// Resolver permits deterministic testing and controlled production DNS.
type Resolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

type netResolver struct{ value *net.Resolver }

func (resolver netResolver) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	return resolver.value.LookupNetIP(ctx, network, host)
}
