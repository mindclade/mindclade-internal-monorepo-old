// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package servicekit

import (
	"context"
	"reflect"
	"sync"
	"testing"
)

func TestAssemblyOrdersStagesAndPreservesRegistrationOrder(t *testing.T) {
	assembly, err := NewAssembly("control-plane")
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []struct {
		stage Stage
		name  string
	}{
		{StageServing, "grpc"},
		{StageInfrastructure, "postgres"},
		{StageFoundation, "telemetry"},
		{StageWork, "projector"},
		{StageInfrastructure, "migrations"},
		{StageCoordination, "outbox"},
	} {
		if err := assembly.Add(value.stage, Component{Name: value.name, Start: func(context.Context) error { return nil }}); err != nil {
			t.Fatal(err)
		}
	}
	service, err := assembly.Build()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"telemetry", "postgres", "migrations", "outbox", "projector", "grpc"}
	if got := service.Components(); !reflect.DeepEqual(got, want) {
		t.Fatalf("components=%v want=%v", got, want)
	}
}

func TestAssemblyLifecycleStopsInReverseStageOrder(t *testing.T) {
	assembly, err := NewAssembly("scheduler")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var events []string
	component := func(name string) Component {
		return Component{
			Name: name,
			Start: func(context.Context) error {
				mu.Lock()
				events = append(events, "start:"+name)
				mu.Unlock()
				return nil
			},
			Stop: func(context.Context) error { mu.Lock(); events = append(events, "stop:"+name); mu.Unlock(); return nil },
		}
	}
	if err := assembly.AddFoundation(component("telemetry")); err != nil {
		t.Fatal(err)
	}
	if err := assembly.AddInfrastructure(component("database")); err != nil {
		t.Fatal(err)
	}
	if err := assembly.AddServing(component("api")); err != nil {
		t.Fatal(err)
	}
	service, err := assembly.Build()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- service.Run(ctx) }()
	for service.Snapshot().State != StateRunning {
		select {
		case err := <-done:
			t.Fatalf("service exited before running: %v", err)
		default:
		}
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	want := []string{"start:telemetry", "start:database", "start:api", "stop:api", "stop:database", "stop:telemetry"}
	mu.Lock()
	got := append([]string(nil), events...)
	mu.Unlock()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("events=%v want=%v", got, want)
	}
}

func TestAssemblyRejectsDuplicateAndMutationAfterBuild(t *testing.T) {
	assembly, err := NewAssembly("registry")
	if err != nil {
		t.Fatal(err)
	}
	component := Component{Name: "database", Start: func(context.Context) error { return nil }}
	if err := assembly.AddInfrastructure(component); err != nil {
		t.Fatal(err)
	}
	if err := assembly.AddInfrastructure(component); err == nil {
		t.Fatal("duplicate accepted")
	}
	if _, err := assembly.Build(); err != nil {
		t.Fatal(err)
	}
	if err := assembly.AddWork(Component{Name: "worker", Run: func(context.Context) error { return nil }}); err == nil {
		t.Fatal("mutation after build accepted")
	}
	if _, err := assembly.Build(); err == nil {
		t.Fatal("second build accepted")
	}
}
