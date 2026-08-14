// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package controller

import (
	"context"
	"errors"
	"testing"
)

type fakeManager struct{ started chan struct{} }

func (manager *fakeManager) Start(ctx context.Context) error {
	close(manager.started)
	<-ctx.Done()
	return ctx.Err()
}

func TestManagerRuntimeComponent(t *testing.T) {
	manager := &fakeManager{started: make(chan struct{})}
	runtime, err := NewManagerRuntime(manager, nil)
	if err != nil {
		t.Fatal(err)
	}
	component := runtime.Component("kubernetes-manager")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- component.Run(ctx) }()
	<-manager.started
	if err := component.Readiness(context.Background()); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
