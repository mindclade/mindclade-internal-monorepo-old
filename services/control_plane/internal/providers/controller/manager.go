// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package controller

import (
	"time"

	"k8s.io/client-go/rest"
	ctrlmanager "sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"go.mindclade.dev/libs/go/faults"
)

// managerShutdownTimeout bounds how long the manager may take to stop its
// reconcilers. servicekit already bounds process shutdown; this keeps the
// manager from being the component that consumes the whole budget.
const managerShutdownTimeout = 20 * time.Second

// newManager constructs the controller-runtime manager.
//
// Three of its subsystems are deliberately switched off, because the
// foundation already owns them and running both would mean two answers to the
// same question:
//
//   - Leader election. Singleton authority is coordination/leadership, fenced
//     through the canonical lease contract. controller-runtime's own election
//     uses a different lease, held by a different owner, with a different TTL;
//     a process running both can be the leader by one and not the other.
//   - Metrics serving. observability owns the exporter and the resource
//     attributes that identify this process.
//   - Health probes. servicekit owns readiness and liveness, and the manager
//     is one component within that lifecycle rather than a parallel one.
//
// The scheme is left at the controller-runtime default, which registers the
// built-in Kubernetes types. Domain code adds its own custom resources; a
// composition root has no types of its own to register.
func newManager(config *rest.Config) (ctrlmanager.Manager, error) {
	if config == nil {
		return nil, faults.New(
			faults.CodeFailedPrecondition,
			"Kubernetes REST configuration is required to build a manager",
			faults.WithReason("nil_rest_configuration"),
			faults.WithOperation("controlplane.controller.newManager"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	timeout := managerShutdownTimeout
	manager, err := ctrlmanager.New(config, ctrlmanager.Options{
		LeaderElection:          false,
		Metrics:                 metricsserver.Options{BindAddress: "0"},
		HealthProbeBindAddress:  "0",
		GracefulShutdownTimeout: &timeout,
	})
	if err != nil {
		return nil, faults.Wrap(err, faults.CodeFailedPrecondition,
			"unable to construct the Kubernetes controller manager",
			faults.WithReason("kubernetes_manager_construction_failed"),
			faults.WithOperation("controlplane.controller.newManager"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	return manager, nil
}
