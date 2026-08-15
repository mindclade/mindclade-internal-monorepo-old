// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package servicekit

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestTaskGroupOwnsCancellationAndJoin(t *testing.T) {
	group, err := NewTaskGroup("workers", nil)
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	if err := group.Add("dispatcher", func(ctx context.Context) error { close(entered); <-ctx.Done(); return ctx.Err() }); err != nil {
		t.Fatal(err)
	}
	if err := group.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-entered
	group.Cancel(errors.New("drain"))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	report, err := group.Join(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Complete() || len(report.Tasks) != 1 || report.Tasks[0].State != TaskCanceled {
		t.Fatalf("unexpected report: %#v", report)
	}
}

func TestTaskGroupContainsPanic(t *testing.T) {
	group, _ := NewTaskGroup("workers", nil)
	_ = group.Add("panicking", func(context.Context) error { panic("boom") })
	if err := group.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	err := group.WaitFirst(context.Background())
	var taskErr *TaskError
	if !errors.As(err, &taskErr) {
		t.Fatalf("got %T %v", err, err)
	}
	report, joinErr := group.Join(context.Background())
	if joinErr != nil || len(report.Failures()) != 1 {
		t.Fatalf("report=%#v err=%v", report, joinErr)
	}
}

func TestServiceDrainsBeforeCancelingRunContext(t *testing.T) {
	service, err := New("drain-order", WithShutdownTimeout(time.Second), WithComponentDrainTimeout(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	running := make(chan struct{})
	drained := make(chan struct{})
	canceled := make(chan struct{})
	if err := service.Add(Component{
		Name: "worker",
		Run:  func(ctx context.Context) error { close(running); <-ctx.Done(); close(canceled); return ctx.Err() },
		Drain: func(context.Context) error {
			select {
			case <-canceled:
				t.Fatal("run context canceled before drain")
			default:
			}
			close(drained)
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { result <- service.Run(context.Background()) }()
	<-running
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := service.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	select {
	case <-drained:
	default:
		t.Fatal("drain not called")
	}
	select {
	case <-canceled:
	default:
		t.Fatal("run context not canceled after drain")
	}
}
