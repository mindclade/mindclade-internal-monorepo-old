// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package health

import (
	"context"
	"google.golang.org/grpc"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"go.mindclade.dev/libs/go/servicekit"
	"testing"
	"time"
)

type prober struct{ ok bool }

func (value prober) Readiness(context.Context) servicekit.ProbeReport {
	return servicekit.ProbeReport{OK: value.ok, CheckedAt: time.Now()}
}
func TestSynchronizer(t *testing.T) {
	server := grpc.NewServer()
	synchronizer, err := Register(server, prober{ok: true}, Config{Services: []string{"svc"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := synchronizer.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	response, err := synchronizer.Server().Check(context.Background(), &healthpb.HealthCheckRequest{Service: "svc"})
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != healthpb.HealthCheckResponse_SERVING {
		t.Fatalf("status=%v", response.Status)
	}
}

type pointerProber struct{}

func (*pointerProber) Readiness(context.Context) servicekit.ProbeReport {
	return servicekit.ProbeReport{OK: true}
}

func TestRegisterRejectsInvalidDependencies(t *testing.T) {
	var server *grpc.Server
	if _, err := Register(server, prober{ok: true}, Config{}); err == nil {
		t.Fatal("expected typed-nil registrar error")
	}
	var probe *pointerProber
	if _, err := Register(grpc.NewServer(), probe, Config{}); err == nil {
		t.Fatal("expected typed-nil prober error")
	}
	if _, err := Register(grpc.NewServer(), prober{ok: true}, Config{Services: []string{"bad service"}}); err == nil {
		t.Fatal("expected invalid service error")
	}
}

func TestSynchronizerRejectsNilContext(t *testing.T) {
	synchronizer, err := Register(grpc.NewServer(), prober{ok: true}, Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := synchronizer.Sync(nil); err == nil {
		t.Fatal("expected nil context error")
	}
	if err := synchronizer.Run(nil); err == nil {
		t.Fatal("expected nil context error")
	}
}
