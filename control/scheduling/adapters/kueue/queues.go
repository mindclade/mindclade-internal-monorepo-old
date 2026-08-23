// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package kueue

import (
	"context"
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	"go.mindclade.dev/control/scheduling"
)

// MaximumParsedEntries bounds every list this adapter walks out of an
// unstructured object. Cluster objects are untrusted input to a control-plane
// process the same way a request body is; an object with a million flavor
// entries must fail, not allocate.
const MaximumParsedEntries = 64

// namePrefix is the shared prefix of all three capacity namespaces and queues.
const namePrefix = "mindclade-"

// DomainForObjectName recovers the capacity domain from a queue object name.
// The name IS the workload class prefixed by the estate name, and that is not a
// coincidence to be worked around -- it is the mapping the admission policy
// checks.
func DomainForObjectName(name string) (scheduling.CapacityDomain, error) {
	class, found := strings.CutPrefix(name, namePrefix)
	if !found {
		return scheduling.CapacityDomain{}, invalid("queue_name_unrecognized", "queue name is not a mindclade capacity queue", nil)
	}
	domain, err := scheduling.DomainFor(scheduling.WorkloadClass(class))
	if err != nil {
		return scheduling.CapacityDomain{}, err
	}
	return domain, nil
}

// ClusterQueueState is the projection of one ClusterQueue.
type ClusterQueueState struct {
	Domain     scheduling.CapacityDomain
	Flavor     scheduling.ResourceFlavor
	Nominal    scheduling.Demand
	StopPolicy string
}

// Admitting reports whether this queue can admit anything at all. A held queue
// and a zero-quota queue are both non-admitting, and the distinction matters
// only to the operator reading the reason.
func (state ClusterQueueState) Admitting() bool {
	return state.StopPolicy == StopPolicyNone && !state.Nominal.IsZero()
}

// Allocatable projects the queue onto the scheduling ledger, given the demand
// the scheduler's own records say is currently held.
//
// Reserved comes from the scheduler, not from the cluster, on purpose: Kueue's
// view lags admission by a reconcile, and a ledger built from the lagging view
// would let the same capacity be reserved twice inside that window.
func (state ClusterQueueState) Allocatable(reserved scheduling.Demand) (scheduling.Allocatable, error) {
	allocatable := scheduling.Allocatable{
		Domain:   state.Domain,
		Nominal:  state.Nominal.Clone(),
		Reserved: reserved.Clone(),
	}
	if err := allocatable.Validate(); err != nil {
		return scheduling.Allocatable{}, err
	}
	return allocatable, nil
}

// ProjectClusterQueue parses and verifies one ClusterQueue object.
//
// Verification is strict because the invariants this package relies on are
// carried by the object, not by this code: one ResourceGroup, one flavor, the
// exact covered-resource set for the domain, and preemption disabled in both
// directions. A cluster that stopped satisfying any of them would silently
// invalidate placement and preemption policy, so the projection refuses rather
// than returning a plausible-looking state.
func ProjectClusterQueue(object *unstructured.Unstructured) (ClusterQueueState, error) {
	if object == nil {
		return ClusterQueueState{}, invalid("object_nil", "cluster queue object is required", nil)
	}
	domain, err := DomainForObjectName(object.GetName())
	if err != nil {
		return ClusterQueueState{}, err
	}
	if err := verifyPreemptionDisabled(object); err != nil {
		return ClusterQueueState{}, err
	}
	groups, found, err := unstructured.NestedSlice(object.Object, "spec", "resourceGroups")
	if err != nil || !found {
		return ClusterQueueState{}, invalid("cluster_queue_resource_groups_missing", "cluster queue declares no resource groups", err)
	}
	// More than one group would let a workload draw CPU selectors from one
	// flavor and GPU selectors from another, which is the exact split the
	// single-group manifest exists to prevent.
	if len(groups) != 1 {
		return ClusterQueueState{}, failedPrecondition("cluster_queue_group_count", "cluster queue must declare exactly one resource group")
	}
	group, ok := groups[0].(map[string]any)
	if !ok {
		return ClusterQueueState{}, invalid("cluster_queue_group_invalid", "cluster queue resource group is malformed", nil)
	}
	covered, err := parseStringList(group, "coveredResources")
	if err != nil {
		return ClusterQueueState{}, err
	}
	if err := verifyCoveredResources(domain, covered); err != nil {
		return ClusterQueueState{}, err
	}
	flavors, found, err := unstructured.NestedSlice(group, "flavors")
	if err != nil || !found {
		return ClusterQueueState{}, invalid("cluster_queue_flavors_missing", "cluster queue resource group declares no flavors", err)
	}
	if len(flavors) != 1 {
		return ClusterQueueState{}, failedPrecondition("cluster_queue_flavor_count", "cluster queue resource group must declare exactly one flavor")
	}
	flavor, ok := flavors[0].(map[string]any)
	if !ok {
		return ClusterQueueState{}, invalid("cluster_queue_flavor_invalid", "cluster queue flavor is malformed", nil)
	}
	flavorName, found, err := unstructured.NestedString(flavor, "name")
	if err != nil || !found {
		return ClusterQueueState{}, invalid("cluster_queue_flavor_name_missing", "cluster queue flavor has no name", err)
	}
	if scheduling.ResourceFlavor(flavorName) != domain.Flavor() {
		return ClusterQueueState{}, failedPrecondition("cluster_queue_flavor_mismatch", "cluster queue flavor does not match the capacity domain")
	}
	nominal, err := parseNominalQuota(flavor, domain)
	if err != nil {
		return ClusterQueueState{}, err
	}
	stopPolicy, _, err := unstructured.NestedString(object.Object, "spec", "stopPolicy")
	if err != nil {
		return ClusterQueueState{}, invalid("cluster_queue_stop_policy_invalid", "cluster queue stop policy is malformed", err)
	}
	if stopPolicy == "" {
		stopPolicy = StopPolicyNone
	}
	state := ClusterQueueState{Domain: domain, Flavor: domain.Flavor(), Nominal: nominal, StopPolicy: stopPolicy}
	if _, err := state.Allocatable(nil); err != nil {
		return ClusterQueueState{}, err
	}
	return state, nil
}

// verifyPreemptionDisabled asserts the fact preemption.go is written against.
// If Kueue ever starts reclaiming, Go-side victim selection would be racing it,
// and two independent preemptors evicting for the same shortfall evict twice as
// much as either intended.
func verifyPreemptionDisabled(object *unstructured.Unstructured) error {
	for _, field := range []string{"reclaimWithinCohort", "withinClusterQueue"} {
		value, found, err := unstructured.NestedString(object.Object, "spec", "preemption", field)
		if err != nil {
			return invalid("cluster_queue_preemption_invalid", "cluster queue preemption policy is malformed", err)
		}
		if !found || value != "Never" {
			return failedPrecondition("cluster_queue_preemption_enabled", "cluster queue preemption must be Never in both directions")
		}
	}
	return nil
}

func verifyCoveredResources(domain scheduling.CapacityDomain, covered []string) error {
	expected := domain.CoveredResources()
	if len(covered) != len(expected) {
		return failedPrecondition("cluster_queue_covered_mismatch", "cluster queue covered resources do not match the capacity domain")
	}
	seen := make(map[scheduling.ResourceName]struct{}, len(covered))
	for _, name := range covered {
		resourceName := scheduling.ResourceName(name)
		if !resourceName.Valid() {
			return failedPrecondition("cluster_queue_covered_unknown", "cluster queue covers a resource outside the modelled group")
		}
		seen[resourceName] = struct{}{}
	}
	for _, name := range expected {
		if _, exists := seen[name]; !exists {
			return failedPrecondition("cluster_queue_covered_mismatch", "cluster queue covered resources do not match the capacity domain")
		}
	}
	return nil
}

func parseNominalQuota(flavor map[string]any, domain scheduling.CapacityDomain) (scheduling.Demand, error) {
	entries, found, err := unstructured.NestedSlice(flavor, "resources")
	if err != nil || !found {
		return nil, invalid("cluster_queue_resources_missing", "cluster queue flavor declares no resources", err)
	}
	if len(entries) > MaximumParsedEntries {
		return nil, failedPrecondition("cluster_queue_resource_bound", "cluster queue flavor declares more resources than the parser bound")
	}
	nominal := make(scheduling.Demand, len(entries))
	for _, entry := range entries {
		fields, ok := entry.(map[string]any)
		if !ok {
			return nil, invalid("cluster_queue_resource_invalid", "cluster queue resource entry is malformed", nil)
		}
		name, found, err := unstructured.NestedString(fields, "name")
		if err != nil || !found {
			return nil, invalid("cluster_queue_resource_name_missing", "cluster queue resource entry has no name", err)
		}
		resourceName := scheduling.ResourceName(name)
		if !resourceName.Valid() || !domain.Covers(resourceName) {
			return nil, failedPrecondition("cluster_queue_resource_uncovered", "cluster queue grants a resource its group does not cover")
		}
		if _, exists := nominal[resourceName]; exists {
			return nil, invalid("cluster_queue_resource_duplicate", "cluster queue repeats a resource", nil)
		}
		amount, err := parseAmount(resourceName, fields["nominalQuota"])
		if err != nil {
			return nil, err
		}
		nominal[resourceName] = amount
	}
	if err := nominal.ValidateForDomain(domain, false); err != nil {
		return nil, err
	}
	return nominal, nil
}

// parseAmount converts a Kubernetes quantity into this package's integer unit
// for that resource: millicores for CPU, bytes for the two storage dimensions,
// and whole devices or pods for the counts.
//
// Rounding is not permitted. A fractional GPU or a fractional pod is not a
// quantity the cluster can grant, and silently truncating one would produce a
// ledger that disagrees with the queue by an amount nobody can see.
func parseAmount(name scheduling.ResourceName, value any) (uint64, error) {
	quantity, err := parseQuantity(value)
	if err != nil {
		return 0, err
	}
	if quantity.Sign() < 0 {
		return 0, invalid("quota_negative", "quota amount is negative", nil)
	}
	var scaled int64
	switch name {
	case scheduling.ResourceCPU:
		scaled = quantity.MilliValue()
	default:
		if quantity.MilliValue()%1000 != 0 {
			return 0, invalid("quota_fractional", "quota amount is fractional for a whole-unit resource", nil)
		}
		scaled = quantity.Value()
	}
	if scaled < 0 || uint64(scaled) > scheduling.MaximumResourceAmount {
		return 0, invalid("quota_out_of_range", "quota amount is outside bounds", nil)
	}
	return uint64(scaled), nil
}

func parseQuantity(value any) (resource.Quantity, error) {
	switch typed := value.(type) {
	case nil:
		return resource.Quantity{}, invalid("quota_missing", "quota amount is required", nil)
	case string:
		quantity, err := resource.ParseQuantity(typed)
		if err != nil {
			return resource.Quantity{}, invalid("quota_unparseable", "quota amount is not a valid quantity", err)
		}
		return quantity, nil
	case int64:
		return *resource.NewQuantity(typed, resource.DecimalSI), nil
	default:
		return resource.Quantity{}, invalid("quota_type_invalid", "quota amount is not a quantity", nil)
	}
}

func parseStringList(fields map[string]any, key string) ([]string, error) {
	raw, found, err := unstructured.NestedSlice(fields, key)
	if err != nil || !found {
		return nil, invalid("string_list_missing", "expected string list is missing", err)
	}
	if len(raw) > MaximumParsedEntries {
		return nil, failedPrecondition("string_list_bound", "string list exceeds the parser bound")
	}
	values := make([]string, 0, len(raw))
	for _, entry := range raw {
		value, ok := entry.(string)
		if !ok {
			return nil, invalid("string_list_entry_invalid", "string list contains a non-string entry", nil)
		}
		values = append(values, value)
	}
	return values, nil
}

// LocalQueueState is the projection of one namespace-local queue.
type LocalQueueState struct {
	Domain       scheduling.CapacityDomain
	ClusterQueue string
	StopPolicy   string
}

// Admitting reports whether the local queue forwards work.
func (state LocalQueueState) Admitting() bool { return state.StopPolicy == StopPolicyNone }

// ProjectLocalQueue parses and verifies one LocalQueue object, including that
// its namespace, its own name, and the ClusterQueue it points at all agree with
// the same capacity triple.
func ProjectLocalQueue(object *unstructured.Unstructured) (LocalQueueState, error) {
	if object == nil {
		return LocalQueueState{}, invalid("object_nil", "local queue object is required", nil)
	}
	domain, err := DomainForObjectName(object.GetName())
	if err != nil {
		return LocalQueueState{}, err
	}
	if object.GetNamespace() != domain.Namespace() {
		return LocalQueueState{}, failedPrecondition("local_queue_namespace_mismatch", "local queue namespace does not match its capacity triple")
	}
	clusterQueue, found, err := unstructured.NestedString(object.Object, "spec", "clusterQueue")
	if err != nil || !found {
		return LocalQueueState{}, invalid("local_queue_cluster_queue_missing", "local queue names no cluster queue", err)
	}
	if clusterQueue != domain.ClusterQueue() {
		return LocalQueueState{}, failedPrecondition("local_queue_cluster_queue_mismatch", "local queue points at a different cluster queue")
	}
	stopPolicy, _, err := unstructured.NestedString(object.Object, "spec", "stopPolicy")
	if err != nil {
		return LocalQueueState{}, invalid("local_queue_stop_policy_invalid", "local queue stop policy is malformed", err)
	}
	if stopPolicy == "" {
		stopPolicy = StopPolicyNone
	}
	return LocalQueueState{Domain: domain, ClusterQueue: clusterQueue, StopPolicy: stopPolicy}, nil
}

// ResourceFlavorState is the projection of one ResourceFlavor.
type ResourceFlavorState struct {
	Name         scheduling.ResourceFlavor
	NodeLabels   map[string]string
	TopologyName string
}

// ProjectResourceFlavor parses one ResourceFlavor and asserts the topology
// binding: the two GPU flavors reference the declared Topology and the CPU
// flavor references none. A CPU flavor that gained a topology would make
// topology-constrained CPU placements suddenly meaningful, which no part of
// this package is written for.
func ProjectResourceFlavor(object *unstructured.Unstructured) (ResourceFlavorState, error) {
	if object == nil {
		return ResourceFlavorState{}, invalid("object_nil", "resource flavor object is required", nil)
	}
	flavor := scheduling.ResourceFlavor(object.GetName())
	switch flavor {
	case scheduling.FlavorCPUGeneralOnDemand, scheduling.FlavorH100, scheduling.FlavorB200:
	default:
		return ResourceFlavorState{}, invalid("resource_flavor_unrecognized", "resource flavor is not one of the three declared flavors", nil)
	}
	labels, _, err := unstructured.NestedStringMap(object.Object, "spec", "nodeLabels")
	if err != nil {
		return ResourceFlavorState{}, invalid("resource_flavor_labels_invalid", "resource flavor node labels are malformed", err)
	}
	if len(labels) > MaximumParsedEntries {
		return ResourceFlavorState{}, failedPrecondition("resource_flavor_label_bound", "resource flavor declares more node labels than the parser bound")
	}
	topologyName, _, err := unstructured.NestedString(object.Object, "spec", "topologyName")
	if err != nil {
		return ResourceFlavorState{}, invalid("resource_flavor_topology_invalid", "resource flavor topology name is malformed", err)
	}
	if flavor.TopologyAware() && topologyName != scheduling.TopologyName {
		return ResourceFlavorState{}, failedPrecondition("resource_flavor_topology_mismatch", "accelerator flavor does not reference the declared topology")
	}
	if !flavor.TopologyAware() && topologyName != "" {
		return ResourceFlavorState{}, failedPrecondition("resource_flavor_topology_unexpected", "non-accelerator flavor declares a topology")
	}
	return ResourceFlavorState{Name: flavor, NodeLabels: labels, TopologyName: topologyName}, nil
}

// TopologyState is the projection of the single Topology object.
type TopologyState struct {
	Name   string
	Levels []scheduling.TopologyLevel
}

// Verify checks the observed topology against the compiled two-level contract.
func (state TopologyState) Verify() error {
	return scheduling.VerifyTopology(state.Name, state.Levels)
}

// ProjectTopology parses the Topology object's ordered level list.
func ProjectTopology(object *unstructured.Unstructured) (TopologyState, error) {
	if object == nil {
		return TopologyState{}, invalid("object_nil", "topology object is required", nil)
	}
	entries, found, err := unstructured.NestedSlice(object.Object, "spec", "levels")
	if err != nil || !found {
		return TopologyState{}, invalid("topology_levels_missing", "topology declares no levels", err)
	}
	if len(entries) > MaximumParsedEntries {
		return TopologyState{}, failedPrecondition("topology_level_bound", "topology declares more levels than the parser bound")
	}
	levels := make([]scheduling.TopologyLevel, 0, len(entries))
	for _, entry := range entries {
		fields, ok := entry.(map[string]any)
		if !ok {
			return TopologyState{}, invalid("topology_level_invalid", "topology level entry is malformed", nil)
		}
		label, found, err := unstructured.NestedString(fields, "nodeLabel")
		if err != nil || !found {
			return TopologyState{}, invalid("topology_level_label_missing", "topology level has no node label", err)
		}
		levels = append(levels, scheduling.TopologyLevel(label))
	}
	return TopologyState{Name: object.GetName(), Levels: levels}, nil
}

// ClusterQueue reads and projects the ClusterQueue backing one capacity domain.
func (client Client) ClusterQueue(ctx context.Context, domain scheduling.CapacityDomain) (ClusterQueueState, error) {
	if err := domain.Validate(); err != nil {
		return ClusterQueueState{}, err
	}
	object, err := client.get(ctx, ClusterQueueGVK, types.NamespacedName{Name: domain.ClusterQueue()})
	if err != nil {
		return ClusterQueueState{}, err
	}
	return ProjectClusterQueue(object)
}

// LocalQueue reads and projects the namespace-local queue for one domain.
func (client Client) LocalQueue(ctx context.Context, domain scheduling.CapacityDomain) (LocalQueueState, error) {
	if err := domain.Validate(); err != nil {
		return LocalQueueState{}, err
	}
	key := types.NamespacedName{Namespace: domain.Namespace(), Name: domain.QueueName()}
	object, err := client.get(ctx, LocalQueueGVK, key)
	if err != nil {
		return LocalQueueState{}, err
	}
	return ProjectLocalQueue(object)
}

// ResourceFlavor reads and projects one flavor.
func (client Client) ResourceFlavor(ctx context.Context, flavor scheduling.ResourceFlavor) (ResourceFlavorState, error) {
	object, err := client.get(ctx, ResourceFlavorGVK, types.NamespacedName{Name: flavor.String()})
	if err != nil {
		return ResourceFlavorState{}, err
	}
	return ProjectResourceFlavor(object)
}

// Topology reads, projects, and verifies the declared topology.
func (client Client) Topology(ctx context.Context) (TopologyState, error) {
	object, err := client.get(ctx, TopologyGVK, types.NamespacedName{Name: scheduling.TopologyName})
	if err != nil {
		return TopologyState{}, err
	}
	state, err := ProjectTopology(object)
	if err != nil {
		return TopologyState{}, err
	}
	if err := state.Verify(); err != nil {
		return TopologyState{}, err
	}
	return state, nil
}
