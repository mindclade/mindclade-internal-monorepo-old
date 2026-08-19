// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package cluster constructs the Kubernetes clients for the roles that need
// them. It is a sibling of the role factories rather than part of providers
// itself: a binary that never talks to an API server must not link a
// Kubernetes client, and packaging decides that, not configuration.
package cluster

import (
	"context"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/discovery"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"

	"go.mindclade.dev/libs/go/faults"
	kubeclient "go.mindclade.dev/libs/go/kubernetes/client"
	"go.mindclade.dev/libs/go/kubernetes/events"
	"go.mindclade.dev/libs/go/servicekit"
	"go.mindclade.dev/services/control_plane/internal/config"
)

// Cluster is the Kubernetes access surface shared by every role whose
// production profile demands CapabilityKubernetes: the scheduler, controller,
// operator, and ingestion coordinator.
//
// One REST configuration is resolved and shared. The controller-runtime client
// and the discovery client are separate because they answer different
// questions -- object reads and writes versus API-server reachability -- and a
// readiness probe must not depend on the cache the reconcilers use.
type Cluster struct {
	Config    *rest.Config
	Client    crclient.Client
	Discovery discovery.DiscoveryInterface
}

// New resolves the REST configuration and constructs the cluster clients. It
// performs no network I/O: reachability is deferred to Readiness so a process
// still starts, and reports itself unready, while the API server is down.
//
// No event recorder is built here. A role that runs a controller-runtime
// manager must take its recorder from the manager, which already owns a
// broadcaster; building a second one would duplicate every event. Roles
// without a manager opt in through Recorder.
func New(ctx context.Context, settings config.Settings, options crclient.Options) (*Cluster, error) {
	if ctx == nil {
		return nil, faults.New(
			faults.CodeInvalidArgument,
			"Kubernetes provider requires a context",
			faults.WithReason("invalid_cluster_request"),
			faults.WithOperation("controlplane.cluster.New"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	restConfig, err := kubeclient.Load(ctx, kubernetesConfig(settings))
	if err != nil {
		return nil, err
	}
	client, err := kubeclient.New(ctx, restConfig, options)
	if err != nil {
		return nil, err
	}
	versions, err := kubeclient.NewDiscovery(ctx, restConfig)
	if err != nil {
		return nil, err
	}
	return &Cluster{Config: restConfig, Client: client, Discovery: versions}, nil
}

// Component is the readiness lifecycle a role registers when it runs no
// controller-runtime manager. CapabilityKubernetes is passive in the
// production vocabulary, so nothing else probes the API server: without this,
// a role whose cluster is unreachable reports ready and silently does no work.
//
// Roles that run a manager pass Readiness to controller.NewManagerRuntime
// instead, and must not also register this.
func (cluster *Cluster) Component(name string) servicekit.Component {
	return servicekit.Component{Name: name, Readiness: cluster.Readiness}
}

// Readiness reports whether the API server is reachable and serving a version.
func (cluster *Cluster) Readiness(ctx context.Context) error {
	if cluster == nil || cluster.Discovery == nil {
		return notConfigured("controlplane.cluster.Cluster.Readiness")
	}
	_, err := kubeclient.Probe(ctx, cluster.Discovery)
	return err
}

// Recorder builds a standalone event recorder and the component that owns the
// event stream, for roles that write cluster events but run no manager.
//
// The broadcaster is constructed here but not started: recording opens a watch
// against the API server, which belongs in the component's Start rather than
// in a constructor a factory calls while assembling.
func (cluster *Cluster) Recorder(ctx context.Context, source string) (*events.Recorder, servicekit.Component, error) {
	const operation = "controlplane.cluster.Cluster.Recorder"
	if cluster == nil || cluster.Config == nil {
		return nil, servicekit.Component{}, notConfigured(operation)
	}
	source = strings.TrimSpace(source)
	if source == "" {
		return nil, servicekit.Component{}, faults.New(
			faults.CodeInvalidArgument,
			"Kubernetes event source is required",
			faults.WithReason("empty_event_source"),
			faults.WithOperation(operation),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	typed, err := kubeclient.NewTyped(ctx, cluster.Config)
	if err != nil {
		return nil, servicekit.Component{}, err
	}
	broadcaster := record.NewBroadcaster()
	recorder, err := events.New(broadcaster.NewRecorder(
		clientgoscheme.Scheme,
		corev1.EventSource{Component: source},
	))
	if err != nil {
		broadcaster.Shutdown()
		return nil, servicekit.Component{}, err
	}

	stream := &eventStream{broadcaster: broadcaster, sink: typed.CoreV1().Events("")}
	return recorder, servicekit.Component{
		Name:  source + "-events",
		Start: stream.start,
		Stop:  stream.stop,
	}, nil
}

// eventStream owns the broadcaster's watch. Servicekit serialises Start and
// Stop for one component, so the handle needs no lock, but Stop must tolerate
// being called before Start and twice: a process that fails partway through
// startup is unwound from whatever point it reached.
type eventStream struct {
	broadcaster record.EventBroadcaster
	sink        typedcorev1.EventInterface
	recording   interface{ Stop() }
}

func (stream *eventStream) start(context.Context) error {
	if stream == nil || stream.broadcaster == nil {
		return notConfigured("controlplane.cluster.eventStream.start")
	}
	stream.recording = stream.broadcaster.StartRecordingToSink(
		&typedcorev1.EventSinkImpl{Interface: stream.sink},
	)
	return nil
}

func (stream *eventStream) stop(context.Context) error {
	if stream == nil {
		return nil
	}
	// Stop the watch before shutting the broadcaster down: the reverse order
	// drops events already queued for the sink.
	if stream.recording != nil {
		stream.recording.Stop()
		stream.recording = nil
	}
	if stream.broadcaster != nil {
		stream.broadcaster.Shutdown()
		stream.broadcaster = nil
	}
	return nil
}

func notConfigured(operation string) error {
	return faults.New(
		faults.CodeFailedPrecondition,
		"Kubernetes cluster provider is not configured",
		faults.WithReason("kubernetes_discovery_not_configured"),
		faults.WithOperation(operation),
		faults.WithRetryPolicy(faults.NoRetry()),
	)
}

// kubernetesConfig maps control-plane settings onto the library contract. QPS
// and burst are deliberately not configurable: they are properties of the
// qualified client, and a role that needs a different budget needs a reviewed
// change to the library default rather than a deployment-time override.
func kubernetesConfig(settings config.Settings) kubeclient.Config {
	resolved := kubeclient.DefaultConfig()
	resolved.Source = kubeclient.Source(strings.TrimSpace(settings.KubernetesSource))
	resolved.KubeconfigPath = strings.TrimSpace(settings.KubernetesKubeconfig)
	resolved.Context = strings.TrimSpace(settings.KubernetesContext)
	resolved.UserAgent = settings.ServiceName
	resolved.Timeout = settings.KubernetesTimeout
	return resolved
}
