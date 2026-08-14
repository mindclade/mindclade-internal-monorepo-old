// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package reflection

import (
	"reflect"

	grpcreflection "google.golang.org/grpc/reflection"

	"mindclade.internal/libs/go/faults"
)

type Config struct{ V1Only bool }

// Register explicitly enables the standard gRPC reflection service. It returns
// an error for nil registrars instead of allowing the upstream helper to panic.
func Register(server grpcreflection.GRPCServer, config Config) error {
	if nilServer(server) {
		return faults.New(faults.CodeInvalidArgument, "gRPC reflection server is required", faults.WithReason("nil_grpc_reflection_server"))
	}
	if config.V1Only {
		grpcreflection.RegisterV1(server)
		return nil
	}
	grpcreflection.Register(server)
	return nil
}

func nilServer(server grpcreflection.GRPCServer) bool {
	if server == nil {
		return true
	}
	value := reflect.ValueOf(server)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
