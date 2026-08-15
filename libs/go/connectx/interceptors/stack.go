// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package interceptors

import "connectrpc.com/connect"

// ServerConfig builds Mindclade's recommended handler interceptor stack. The
// first interceptor is outermost. Outer interceptors run after request metadata
// is established but before fault translation, which is appropriate for server
// telemetry. Inner and Additional interceptors run after authentication,
// authorization, and validation.
type ServerConfig struct {
	Outer          []connect.Interceptor
	PanicReporter  PanicReporter
	Authentication *AuthenticationConfig
	Authorization  *AuthorizationConfig
	Validate       bool
	Inner          []connect.Interceptor

	// Additional is retained as a compatibility alias for Inner. New code
	// should use Inner to make ordering explicit.
	Additional []connect.Interceptor
}

func Server(config ServerConfig) []connect.Interceptor {
	interceptors := []connect.Interceptor{RequestMetadata()}
	interceptors = appendNonNil(interceptors, config.Outer...)
	interceptors = append(interceptors, FaultTranslation(), Recovery(config.PanicReporter))
	if config.Authentication != nil {
		interceptors = append(interceptors, Authentication(*config.Authentication))
	}
	if config.Authorization != nil {
		interceptors = append(interceptors, Authorization(*config.Authorization))
	}
	if config.Validate {
		interceptors = append(interceptors, Validation())
	}
	interceptors = appendNonNil(interceptors, config.Inner...)
	interceptors = appendNonNil(interceptors, config.Additional...)
	return interceptors
}

// ClientConfig builds the client-side stack. Outer interceptors run between
// request metadata and fault translation. Inner interceptors run closest to the
// transport and therefore observe canonical Connect wire errors; client
// telemetry normally belongs in Inner.
type ClientConfig struct {
	Outer []connect.Interceptor
	Inner []connect.Interceptor
}

func ClientWithConfig(config ClientConfig) []connect.Interceptor {
	interceptors := []connect.Interceptor{RequestMetadata()}
	interceptors = appendNonNil(interceptors, config.Outer...)
	interceptors = append(interceptors, FaultTranslation())
	interceptors = appendNonNil(interceptors, config.Inner...)
	return interceptors
}

// Client returns the standard client stack and places additional interceptors
// closest to the transport for backward compatibility.
func Client(additional ...connect.Interceptor) []connect.Interceptor {
	return ClientWithConfig(ClientConfig{Inner: additional})
}

func appendNonNil(target []connect.Interceptor, values ...connect.Interceptor) []connect.Interceptor {
	for _, interceptor := range values {
		if interceptor != nil {
			target = append(target, interceptor)
		}
	}
	return target
}
