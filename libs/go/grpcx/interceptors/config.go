// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package interceptors

import "google.golang.org/grpc"

type ServerConfig struct {
	PanicObserver    PanicObserver
	Authentication   *AuthenticationConfig
	Authorization    *AuthorizationConfig
	ValidateMessages bool
	AdditionalUnary  []grpc.UnaryServerInterceptor
	AdditionalStream []grpc.StreamServerInterceptor
}

// Server returns the recommended chain. Request metadata is established first,
// fault translation serializes all inner failures, and recovery contains panics
// with the derived request context.
func Server(config ServerConfig) ([]grpc.UnaryServerInterceptor, []grpc.StreamServerInterceptor) {
	unary := []grpc.UnaryServerInterceptor{UnaryRequestMetadata(), UnaryFaultTranslation(), UnaryRecovery(config.PanicObserver)}
	stream := []grpc.StreamServerInterceptor{StreamRequestMetadata(), StreamFaultTranslation(), StreamRecovery(config.PanicObserver)}
	if config.Authentication != nil {
		unary = append(unary, UnaryAuthentication(*config.Authentication))
		stream = append(stream, StreamAuthentication(*config.Authentication))
	}
	if config.Authorization != nil {
		unary = append(unary, UnaryAuthorization(*config.Authorization))
		stream = append(stream, StreamAuthorization(*config.Authorization))
	}
	if config.ValidateMessages {
		unary = append(unary, UnaryValidation())
		stream = append(stream, StreamValidation())
	}
	for _, interceptor := range config.AdditionalUnary {
		if interceptor != nil {
			unary = append(unary, interceptor)
		}
	}
	for _, interceptor := range config.AdditionalStream {
		if interceptor != nil {
			stream = append(stream, interceptor)
		}
	}
	return unary, stream
}
func Client(additionalUnary []grpc.UnaryClientInterceptor, additionalStream []grpc.StreamClientInterceptor) ([]grpc.UnaryClientInterceptor, []grpc.StreamClientInterceptor) {
	unary := []grpc.UnaryClientInterceptor{UnaryClientRequestMetadata(), UnaryClientFaultTranslation()}
	stream := []grpc.StreamClientInterceptor{StreamClientRequestMetadata(), StreamClientFaultTranslation()}
	for _, interceptor := range additionalUnary {
		if interceptor != nil {
			unary = append(unary, interceptor)
		}
	}
	for _, interceptor := range additionalStream {
		if interceptor != nil {
			stream = append(stream, interceptor)
		}
	}
	return unary, stream
}
