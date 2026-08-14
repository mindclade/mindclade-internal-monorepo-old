// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package health

import (
	"context"
	"reflect"
	"sort"
	"strings"

	"connectrpc.com/connect"
	"connectrpc.com/grpchealth"

	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/servicekit"
)

type ReadinessProber interface {
	Readiness(context.Context) servicekit.ProbeReport
}

type Checker struct {
	prober   ReadinessProber
	services map[string]struct{}
}

func NewChecker(prober ReadinessProber, services ...string) (*Checker, error) {
	if nilInterface(prober) {
		return nil, faults.New(faults.CodeInvalidArgument, "nil readiness prober", faults.WithReason("nil_readiness_prober"))
	}
	registered := make(map[string]struct{}, len(services))
	for _, service := range services {
		name := strings.TrimSpace(service)
		if name == "" {
			return nil, faults.New(faults.CodeInvalidArgument, "invalid health service name", faults.WithReason("invalid_health_service"))
		}
		registered[name] = struct{}{}
	}
	return &Checker{prober: prober, services: registered}, nil
}

func (checker *Checker) Check(ctx context.Context, request *grpchealth.CheckRequest) (*grpchealth.CheckResponse, error) {
	if checker == nil || nilInterface(checker.prober) {
		return nil, connect.NewError(connect.CodeFailedPrecondition, faults.New(faults.CodeFailedPrecondition, "health checker is not configured"))
	}
	if ctx == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, faults.New(faults.CodeInvalidArgument, "health context is required", faults.WithReason("nil_context")))
	}
	service := ""
	if request != nil {
		service = strings.TrimSpace(request.Service)
	}
	if service != "" {
		if _, ok := checker.services[service]; !ok {
			return nil, connect.NewError(connect.CodeNotFound, faults.New(faults.CodeNotFound, "unknown health service"))
		}
	}
	report := checker.prober.Readiness(ctx)
	status := grpchealth.StatusServing
	if !report.OK {
		status = grpchealth.StatusNotServing
	}
	return &grpchealth.CheckResponse{Status: status}, nil
}

func (checker *Checker) Services() []string {
	if checker == nil {
		return nil
	}
	values := make([]string, 0, len(checker.services))
	for service := range checker.services {
		values = append(values, service)
	}
	sort.Strings(values)
	return values
}

func nilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
