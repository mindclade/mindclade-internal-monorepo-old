// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package middleware

import "net/http"

// StackConfig defines the canonical Mindclade HTTP middleware order. Request
// metadata is outermost so all later middleware sees validated lineage. Access
// observation surrounds recovery so contained panics are recorded as 500s.
type StackConfig struct {
	OperationResolver OperationResolver
	AccessObserver    AccessObserver
	Security          SecurityHeadersConfig
	PanicObserver     PanicObserver
	MaximumBodyBytes  int64
	Authentication    *AuthenticationConfig
	Authorization     *AuthorizationConfig
	Additional        []Middleware
}

// Server applies the canonical server middleware stack to handler.
func Server(handler http.Handler, config StackConfig) http.Handler {
	values := []Middleware{
		RequestMetadata(config.OperationResolver),
		Access(config.AccessObserver),
		SecurityHeaders(config.Security),
		Recovery(config.PanicObserver),
	}
	if config.MaximumBodyBytes > 0 {
		values = append(values, MaximumBody(config.MaximumBodyBytes))
	}
	if config.Authentication != nil {
		values = append(values, Authentication(*config.Authentication))
	}
	if config.Authorization != nil {
		values = append(values, Authorization(*config.Authorization))
	}
	for _, value := range config.Additional {
		if value != nil {
			values = append(values, value)
		}
	}
	return Chain(handler, values...)
}
