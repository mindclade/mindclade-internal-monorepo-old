// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package kueue

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	"go.mindclade.dev/control/scheduling"
)

// Kueue workload condition types this adapter reads.
const (
	ConditionQuotaReserved = "QuotaReserved"
	ConditionAdmitted      = "Admitted"
	ConditionEvicted       = "Evicted"
	ConditionFinished      = "Finished"
)

// WorkloadLabels returns the labels a Job or JobSet must carry.
//
// The queue-name and workload-class values come from the sealed placement's
// capacity domain, never from a caller-supplied string. That is the whole
// reason CapacityDomain exists: the cluster's ValidatingAdmissionPolicy denies
// any object whose namespace, queue name, and workload class are not one of
// three exact triples, and a label map assembled from three separate arguments
// is precisely the shape that gets two of them right.
func WorkloadLabels(placement scheduling.Placement) (map[string]string, error) {
	labels, err := scheduling.QueueLabels(placement)
	if err != nil {
		return nil, err
	}
	if !placement.Priority.Valid() {
		return nil, invalid("priority_class_invalid", "placement priority class is invalid", nil)
	}
	labels[PriorityClassLabel] = placement.Priority.String()
	return labels, nil
}

// Namespace returns the only namespace this placement may be created in.
func Namespace(placement scheduling.Placement) (string, error) {
	if err := placement.Validate(); err != nil {
		return "", err
	}
	return placement.Pool.Domain.Namespace(), nil
}

// PodSetAnnotations returns the topology annotations for a pod template.
//
// At most one annotation is produced. Kueue treats required and preferred as
// mutually exclusive, and emitting both would leave the effective behaviour
// dependent on which one its webhook read first.
func PodSetAnnotations(placement scheduling.Placement) (map[string]string, error) {
	if err := placement.Validate(); err != nil {
		return nil, err
	}
	annotations := map[string]string{}
	switch {
	case placement.Topology.Required != scheduling.TopologyLevelNone:
		annotations[RequiredTopologyAnnotation] = placement.Topology.Required.String()
	case placement.Topology.Preferred != scheduling.TopologyLevelNone:
		annotations[PreferredTopologyAnnotation] = placement.Topology.Preferred.String()
	}
	return annotations, nil
}

// WorkloadState is the projection of one Kueue Workload's status.
type WorkloadState struct {
	Domain        scheduling.CapacityDomain
	QuotaReserved bool
	Admitted      bool
	Evicted       bool
	Finished      bool
	Assignment    scheduling.TopologyAssignment
}

// Bindable reports whether the observed workload is one a reservation may be
// bound against: quota reserved, admitted, and neither evicted nor finished.
//
// Evicted is checked because Kueue can evict for reasons this package does not
// author -- a preemption policy change, a queue stop, a node drain -- and
// binding a reservation to an evicted workload would hold capacity for a pod
// that is not running.
func (state WorkloadState) Bindable() bool {
	return state.QuotaReserved && state.Admitted && !state.Evicted && !state.Finished
}

// ProjectWorkload parses one Workload object against the sealed constraint the
// placement was decided with.
//
// The constraint is an argument rather than something inferred from the object
// because the recorded assignment must be validated against what was
// AUTHORIZED, not against what the cluster happened to do. Inferring it from
// the object would make every assignment self-consistent by construction.
func ProjectWorkload(object *unstructured.Unstructured, constraint scheduling.TopologyConstraint) (WorkloadState, error) {
	if object == nil {
		return WorkloadState{}, invalid("object_nil", "workload object is required", nil)
	}
	if err := constraint.Validate(); err != nil {
		return WorkloadState{}, err
	}
	labels := object.GetLabels()
	domain, err := scheduling.DomainFor(scheduling.WorkloadClass(labels[WorkloadClassLabel]))
	if err != nil {
		return WorkloadState{}, err
	}
	if labels[QueueNameLabel] != domain.QueueName() || object.GetNamespace() != domain.Namespace() {
		return WorkloadState{}, failedPrecondition("workload_triple_mismatch", "workload namespace, queue name, and workload class do not form one capacity triple")
	}
	conditions, err := parseConditions(object)
	if err != nil {
		return WorkloadState{}, err
	}
	assignment, err := parseTopologyAssignment(object, constraint)
	if err != nil {
		return WorkloadState{}, err
	}
	state := WorkloadState{
		Domain:        domain,
		QuotaReserved: conditions[ConditionQuotaReserved],
		Admitted:      conditions[ConditionAdmitted],
		Evicted:       conditions[ConditionEvicted],
		Finished:      conditions[ConditionFinished],
		Assignment:    assignment,
	}
	if err := state.Assignment.Validate(); err != nil {
		return WorkloadState{}, err
	}
	return state, nil
}

func parseConditions(object *unstructured.Unstructured) (map[string]bool, error) {
	entries, found, err := unstructured.NestedSlice(object.Object, "status", "conditions")
	if err != nil {
		return nil, invalid("workload_conditions_invalid", "workload conditions are malformed", err)
	}
	conditions := make(map[string]bool, len(entries))
	if !found {
		return conditions, nil
	}
	if len(entries) > MaximumParsedEntries {
		return nil, failedPrecondition("workload_condition_bound", "workload declares more conditions than the parser bound")
	}
	for _, entry := range entries {
		fields, ok := entry.(map[string]any)
		if !ok {
			return nil, invalid("workload_condition_invalid", "workload condition entry is malformed", nil)
		}
		conditionType, foundType, err := unstructured.NestedString(fields, "type")
		if err != nil || !foundType {
			return nil, invalid("workload_condition_type_missing", "workload condition has no type", err)
		}
		status, _, err := unstructured.NestedString(fields, "status")
		if err != nil {
			return nil, invalid("workload_condition_status_invalid", "workload condition status is malformed", err)
		}
		conditions[conditionType] = status == "True"
	}
	return conditions, nil
}

// parseTopologyAssignment reads the domains Kueue assigned, in the level order
// the assignment declares.
//
// The level list is read from the object rather than assumed, and then checked
// against the two compiled levels. A Workload assigned against a replaced
// three-level topology would otherwise be decoded with zone and host meaning
// something else entirely.
func parseTopologyAssignment(object *unstructured.Unstructured, constraint scheduling.TopologyConstraint) (scheduling.TopologyAssignment, error) {
	assignment := scheduling.TopologyAssignment{Constraint: constraint}
	podSets, found, err := unstructured.NestedSlice(object.Object, "status", "admission", "podSetAssignments")
	if err != nil {
		return scheduling.TopologyAssignment{}, invalid("workload_admission_invalid", "workload admission is malformed", err)
	}
	if !found || len(podSets) == 0 {
		return assignment, nil
	}
	if len(podSets) > MaximumParsedEntries {
		return scheduling.TopologyAssignment{}, failedPrecondition("workload_podset_bound", "workload declares more pod sets than the parser bound")
	}
	seen := make(map[string]struct{}, len(podSets))
	for _, entry := range podSets {
		fields, ok := entry.(map[string]any)
		if !ok {
			return scheduling.TopologyAssignment{}, invalid("workload_podset_invalid", "workload pod set assignment is malformed", nil)
		}
		topology, found, err := unstructured.NestedMap(fields, "topologyAssignment")
		if err != nil {
			return scheduling.TopologyAssignment{}, invalid("workload_topology_invalid", "workload topology assignment is malformed", err)
		}
		if !found {
			continue
		}
		levels, err := parseStringList(topology, "levels")
		if err != nil {
			return scheduling.TopologyAssignment{}, err
		}
		parsed := make([]scheduling.TopologyLevel, 0, len(levels))
		for _, level := range levels {
			parsed = append(parsed, scheduling.TopologyLevel(level))
		}
		if err := scheduling.VerifyTopology(scheduling.TopologyName, parsed); err != nil {
			return scheduling.TopologyAssignment{}, err
		}
		domains, found, err := unstructured.NestedSlice(topology, "domains")
		if err != nil {
			return scheduling.TopologyAssignment{}, invalid("workload_topology_domains_invalid", "workload topology domains are malformed", err)
		}
		if !found {
			continue
		}
		if len(domains) > scheduling.MaximumTopologyDomains {
			return scheduling.TopologyAssignment{}, failedPrecondition("workload_topology_domain_bound", "workload assigns more topology domains than the bound")
		}
		for _, domainEntry := range domains {
			domainFields, ok := domainEntry.(map[string]any)
			if !ok {
				return scheduling.TopologyAssignment{}, invalid("workload_topology_domain_invalid", "workload topology domain is malformed", nil)
			}
			values, err := parseStringList(domainFields, "values")
			if err != nil {
				return scheduling.TopologyAssignment{}, err
			}
			if len(values) == 0 || len(values) > len(parsed) {
				return scheduling.TopologyAssignment{}, invalid("workload_topology_domain_arity", "workload topology domain does not match the declared levels", nil)
			}
			domain := scheduling.TopologyDomain{Zone: values[0]}
			if len(values) > 1 {
				domain.Host = values[1]
			}
			key := domain.Zone + "\x1f" + domain.Host
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			assignment.Domains = append(assignment.Domains, domain)
		}
	}
	return assignment, nil
}

// Workload reads and projects one Kueue Workload.
func (client Client) Workload(ctx context.Context, namespace, name string, constraint scheduling.TopologyConstraint) (WorkloadState, error) {
	object, err := client.get(ctx, WorkloadGVK, types.NamespacedName{Namespace: namespace, Name: name})
	if err != nil {
		return WorkloadState{}, err
	}
	return ProjectWorkload(object, constraint)
}
