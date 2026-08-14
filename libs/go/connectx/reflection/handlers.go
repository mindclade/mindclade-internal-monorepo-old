// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package reflection

import (
	"net/http"
	"sort"
	"strings"

	"connectrpc.com/connect"
	"connectrpc.com/grpcreflect"

	"mindclade.internal/libs/go/connectx"
	"mindclade.internal/libs/go/faults"
)

type Mount struct {
	Pattern string
	Handler http.Handler
}

// Handlers returns v1 and optionally v1alpha reflection mounts. Reflection is
// never registered implicitly because it exposes compiled protobuf schemas.
func Handlers(services []string, includeV1Alpha bool, options ...connect.HandlerOption) ([]Mount, error) {
	clean := make([]string, 0, len(services))
	seen := map[string]struct{}{}
	for _, service := range services {
		name := strings.TrimSpace(service)
		if name == "" {
			return nil, faults.New(faults.CodeInvalidArgument, "invalid reflection service name", faults.WithReason("invalid_reflection_service"))
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		clean = append(clean, name)
	}
	if len(clean) == 0 {
		return nil, faults.New(faults.CodeInvalidArgument, "reflection requires at least one service", faults.WithReason("reflection_service_missing"))
	}
	sort.Strings(clean)
	reflector := grpcreflect.NewStaticReflector(clean...)
	pattern, handler := grpcreflect.NewHandlerV1(reflector, options...)
	mounts := []Mount{{Pattern: pattern, Handler: handler}}
	if includeV1Alpha {
		pattern, handler = grpcreflect.NewHandlerV1Alpha(reflector, options...)
		mounts = append(mounts, Mount{Pattern: pattern, Handler: handler})
	}
	return mounts, nil
}

// Register validates the complete mount set before changing mux. Conflicts with
// handlers already present on mux are returned as structured faults instead of
// escaping as net/http panics.
func Register(mux *http.ServeMux, mounts ...Mount) error {
	if mux == nil {
		return faults.New(faults.CodeInvalidArgument, "nil reflection mux", faults.WithReason("nil_reflection_mux"))
	}

	patterns := make(map[string]struct{}, len(mounts))
	for _, mount := range mounts {
		pattern := strings.TrimSpace(mount.Pattern)
		if pattern == "" {
			return faults.New(faults.CodeInvalidArgument, "invalid reflection mount", faults.WithReason("invalid_reflection_mount"))
		}
		if _, duplicate := patterns[pattern]; duplicate {
			return faults.New(faults.CodeAlreadyExists, "duplicate reflection mount", faults.WithReason("duplicate_reflection_mount"))
		}
		patterns[pattern] = struct{}{}
		if err := validateHandler(mount.Handler); err != nil {
			return err
		}
	}

	for _, mount := range mounts {
		if err := connectx.Mount(mux, strings.TrimSpace(mount.Pattern), mount.Handler); err != nil {
			return err
		}
	}
	return nil
}

func validateHandler(handler http.Handler) error {
	// connectx.Mount performs the authoritative typed-nil validation. Use a
	// disposable mux so Register can prevalidate the full set before mutating
	// the caller's mux.
	if err := connectx.Mount(http.NewServeMux(), "/", handler); err != nil {
		return faults.New(faults.CodeInvalidArgument, "invalid reflection mount", faults.WithReason("invalid_reflection_mount"))
	}
	return nil
}
