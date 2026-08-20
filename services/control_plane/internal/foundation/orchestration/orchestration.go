// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package orchestration carries the Kubernetes half of a role's foundation:
// the cluster client every reconciler reads and writes through, the recorder
// that publishes reconcile outcomes as events on the objects themselves, and
// the controller-runtime manager whose lifetime the process owns.
//
// The scheduler, controller, operator, and ingestion-coordinator profiles all
// require CapabilityKubernetes; the controller and operator additionally
// require CapabilityKubernetesManager, which is the only capability here that
// owns a lifecycle component.
package orchestration

import (
	crclient "sigs.k8s.io/controller-runtime/pkg/client"

	"go.mindclade.dev/libs/go/kubernetes/controller"
	"go.mindclade.dev/libs/go/kubernetes/events"
	"go.mindclade.dev/libs/go/servicekit"
	"go.mindclade.dev/libs/go/servicekit/production"
	"go.mindclade.dev/services/control_plane/internal/foundation"
)

// ManagerComponent is the lifecycle name the controller-runtime manager is
// registered under. It is fixed because a process runs at most one manager:
// two would race for the same informer cache and leader lease.
const ManagerComponent = "kubernetes-manager"

// Cluster is the set of Kubernetes providers a role holds. Client is the
// capability itself -- a role that cannot reach the API server cannot
// reconcile. Manager is present only in roles that run reconcilers.
type Cluster struct {
	Client  crclient.Client
	Events  *events.Recorder
	Manager *controller.ManagerRuntime
}

func (cluster Cluster) declarations() []foundation.Declaration {
	var manager *servicekit.Component
	if cluster.Manager != nil {
		component := cluster.Manager.Component(ManagerComponent)
		manager = &component
	}
	// The recorder deliberately declares nothing. Event publication is not a
	// capability in the production vocabulary: it is part of cluster access,
	// and a role that holds a client but records no events is still correct.
	return []foundation.Declaration{
		{Capability: production.CapabilityKubernetes, Present: !foundation.IsNil(cluster.Client)},
		{Capability: production.CapabilityKubernetesManager, Present: manager != nil, Component: manager},
	}
}

func (cluster Cluster) Capabilities() []production.Capability {
	return foundation.Present(cluster.declarations())
}

func (cluster Cluster) ServiceOptions() []servicekit.Option { return nil }

func (cluster Cluster) Register(builder *production.Builder) error {
	return foundation.Register(builder, cluster.declarations())
}
