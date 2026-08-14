// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package controller

import (
	"time"

	runtimecontroller "sigs.k8s.io/controller-runtime/pkg/controller"

	"mindclade.internal/libs/go/faults"
)

// Settings contains stable high-level controller settings. It deliberately
// excludes queue and rate-limiter implementation details.
type Settings struct {
	MaxConcurrentReconciles int
	CacheSyncTimeout        time.Duration
	ReconciliationTimeout   time.Duration
	RecoverPanic            *bool
	NeedLeaderElection      *bool
	UsePriorityQueue        *bool
	EnableWarmup            *bool
}

func (settings Settings) Validate() error {
	if settings.MaxConcurrentReconciles < 0 || settings.CacheSyncTimeout < 0 || settings.ReconciliationTimeout < 0 {
		return faults.New(faults.CodeInvalidArgument, "invalid Kubernetes controller settings", faults.WithReason("invalid_controller_settings"), faults.WithOperation("kubernetes.controller.Settings.Validate"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	if settings.EnableWarmup != nil && *settings.EnableWarmup && settings.NeedLeaderElection != nil && !*settings.NeedLeaderElection {
		return faults.New(faults.CodeInvalidArgument, "controller warmup requires leader election", faults.WithReason("warmup_without_leader_election"), faults.WithOperation("kubernetes.controller.Settings.Validate"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return nil
}

// Apply overlays explicitly configured settings onto controller-runtime
// options without replacing provider-owned defaults for zero values.
func (settings Settings) Apply(options *runtimecontroller.Options) error {
	if options == nil {
		return faults.New(faults.CodeInvalidArgument, "controller options are required", faults.WithReason("nil_controller_options"), faults.WithOperation("kubernetes.controller.Settings.Apply"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	if err := settings.Validate(); err != nil {
		return err
	}
	if settings.MaxConcurrentReconciles > 0 {
		options.MaxConcurrentReconciles = settings.MaxConcurrentReconciles
	}
	if settings.CacheSyncTimeout > 0 {
		options.CacheSyncTimeout = settings.CacheSyncTimeout
	}
	if settings.ReconciliationTimeout > 0 {
		options.ReconciliationTimeout = settings.ReconciliationTimeout
	}
	if settings.RecoverPanic != nil {
		options.RecoverPanic = cloneBool(settings.RecoverPanic)
	}
	if settings.NeedLeaderElection != nil {
		options.NeedLeaderElection = cloneBool(settings.NeedLeaderElection)
	}
	if settings.UsePriorityQueue != nil {
		options.UsePriorityQueue = cloneBool(settings.UsePriorityQueue)
	}
	if settings.EnableWarmup != nil {
		options.EnableWarmup = cloneBool(settings.EnableWarmup)
	}
	return nil
}

func cloneBool(value *bool) *bool {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
