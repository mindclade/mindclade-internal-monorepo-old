// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package health

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"time"

	"google.golang.org/grpc"
	grpc_health "google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	mcclock "mindclade.internal/libs/go/clock"
	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/servicekit"
)

type Prober interface {
	Readiness(context.Context) servicekit.ProbeReport
}

type Synchronizer struct {
	server   *grpc_health.Server
	prober   Prober
	services []string
	interval time.Duration
	clock    mcclock.Clock
}

type Config struct {
	Services []string
	Interval time.Duration
	Clock    mcclock.Clock
}

func Register(registrar grpc.ServiceRegistrar, prober Prober, config Config) (*Synchronizer, error) {
	if nilInterface(registrar) {
		return nil, faults.New(faults.CodeInvalidArgument, "gRPC service registrar is required", faults.WithReason("nil_grpc_registrar"))
	}
	if nilInterface(prober) {
		return nil, faults.New(faults.CodeInvalidArgument, "readiness prober is required", faults.WithReason("nil_readiness_prober"))
	}
	if config.Interval == 0 {
		config.Interval = 5 * time.Second
	}
	if config.Interval <= 0 {
		return nil, faults.New(faults.CodeInvalidArgument, "health synchronization interval must be positive", faults.WithReason("invalid_health_interval"))
	}
	if nilInterface(config.Clock) {
		if config.Clock != nil {
			return nil, faults.New(faults.CodeInvalidArgument, "health clock is invalid", faults.WithReason("nil_health_clock"))
		}
		config.Clock = mcclock.RealClock{}
	}
	services, err := normalizeServices(config.Services)
	if err != nil {
		return nil, err
	}
	server := grpc_health.NewServer()
	healthpb.RegisterHealthServer(registrar, server)
	return &Synchronizer{server: server, prober: prober, services: services, interval: config.Interval, clock: config.Clock}, nil
}

func (synchronizer *Synchronizer) Server() *grpc_health.Server {
	if synchronizer == nil {
		return nil
	}
	return synchronizer.server
}

func (synchronizer *Synchronizer) Sync(ctx context.Context) error {
	if synchronizer == nil || synchronizer.server == nil || nilInterface(synchronizer.prober) {
		return faults.New(faults.CodeFailedPrecondition, "gRPC health synchronizer is not initialized", faults.WithReason("nil_health_synchronizer"))
	}
	if ctx == nil {
		return faults.New(faults.CodeInvalidArgument, "health context is required", faults.WithReason("nil_context"))
	}
	status := healthpb.HealthCheckResponse_NOT_SERVING
	if synchronizer.prober.Readiness(ctx).OK {
		status = healthpb.HealthCheckResponse_SERVING
	}
	synchronizer.server.SetServingStatus("", status)
	for _, service := range synchronizer.services {
		synchronizer.server.SetServingStatus(service, status)
	}
	return nil
}

func (synchronizer *Synchronizer) Run(ctx context.Context) error {
	if ctx == nil {
		return faults.New(faults.CodeInvalidArgument, "health context is required", faults.WithReason("nil_context"))
	}
	if err := synchronizer.Sync(ctx); err != nil {
		return err
	}
	ticker := synchronizer.clock.NewTicker(synchronizer.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			synchronizer.SetNotServing()
			return nil
		case <-ticker.C():
			if err := synchronizer.Sync(ctx); err != nil {
				return err
			}
		}
	}
}

func (synchronizer *Synchronizer) SetNotServing() {
	if synchronizer == nil || synchronizer.server == nil {
		return
	}
	synchronizer.server.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
	for _, service := range synchronizer.services {
		synchronizer.server.SetServingStatus(service, healthpb.HealthCheckResponse_NOT_SERVING)
	}
}

func (synchronizer *Synchronizer) Component(name string) servicekit.Component {
	return servicekit.Component{
		Name:  name,
		Start: func(ctx context.Context) error { return synchronizer.Sync(ctx) },
		Run:   func(ctx context.Context) error { return synchronizer.Run(ctx) },
		Stop:  func(context.Context) error { synchronizer.SetNotServing(); return nil },
	}
}

func normalizeServices(input []string) ([]string, error) {
	set := map[string]struct{}{}
	for _, service := range input {
		service = strings.TrimSpace(service)
		if service == "" || strings.ContainsAny(service, "/\\ \t\r\n") {
			return nil, faults.New(faults.CodeInvalidArgument, "invalid gRPC health service name", faults.WithReason("invalid_health_service"))
		}
		set[service] = struct{}{}
	}
	output := make([]string, 0, len(set))
	for service := range set {
		output = append(output, service)
	}
	sort.Strings(output)
	return output, nil
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
