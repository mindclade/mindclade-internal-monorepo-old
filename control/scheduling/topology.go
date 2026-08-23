// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package scheduling

import (
	"strconv"

	"go.mindclade.dev/libs/go/identifiers"
)

// TopologyName is the only Kueue Topology the cluster declares. Both GPU
// flavors reference it; the CPU flavor references none.
const TopologyName = "mindclade-gpu-zone-host"

// MaximumTopologyDomains bounds a recorded assignment. Topology assignments are
// stored on durable reservations and replayed on every reconcile, so the list
// has to have a ceiling; 4096 is far above the host count of any single
// placement and far below anything that would make a replay expensive.
const MaximumTopologyDomains = 4096

// TopologyLevel is one node label in the topology, ordered widest first.
//
// There are EXACTLY two. The Topology object is immutable after creation, so a
// rack or block level is not something this package can decide to want: adding
// one requires a new Topology object and a measured migration. Modelling a
// third level here would produce constraints the cluster cannot satisfy.
type TopologyLevel string

const (
	TopologyLevelZone TopologyLevel = "topology.kubernetes.io/zone"
	TopologyLevelHost TopologyLevel = "kubernetes.io/hostname"
	// TopologyLevelNone means the placement expresses no topology preference.
	TopologyLevelNone TopologyLevel = ""
)

var topologyLevels = [...]TopologyLevel{TopologyLevelZone, TopologyLevelHost}

// TopologyLevels returns the two levels, widest first.
func TopologyLevels() []TopologyLevel { return append([]TopologyLevel(nil), topologyLevels[:]...) }

func (level TopologyLevel) String() string { return string(level) }

func (level TopologyLevel) Valid() bool {
	return level == TopologyLevelZone || level == TopologyLevelHost
}

// Depth returns the level's index, widest first. It exists so a constraint can
// be compared without a string switch at every call site.
func (level TopologyLevel) Depth() (int, bool) {
	for index, candidate := range topologyLevels {
		if level == candidate {
			return index, true
		}
	}
	return 0, false
}

// TopologyFingerprint seals the topology contract this package was written
// against: the object's name and its ordered level list.
//
// A controller compares this against the fingerprint of the Topology it
// observes. They differ only if someone replaced the immutable object, and a
// scheduler that keeps emitting zone/host constraints against a topology that
// now means something else places work on the wrong hardware while reporting
// success. The fingerprint turns that into a detectable precondition failure.
func TopologyFingerprint() identifiers.Digest {
	values := make([]string, 0, len(topologyLevels)+2)
	values = append(values, "mindclade-topology-v1", TopologyName)
	for _, level := range topologyLevels {
		values = append(values, string(level))
	}
	return identifiers.SHA256String(canonicalJoin(values...))
}

// VerifyTopology checks an observed topology against the compiled contract.
func VerifyTopology(name string, levels []TopologyLevel) error {
	if name != TopologyName {
		return failedPrecondition("topology_name_mismatch", "observed topology is not the compiled topology")
	}
	if len(levels) != len(topologyLevels) {
		return failedPrecondition("topology_depth_mismatch", "observed topology does not have exactly two levels")
	}
	for index, level := range levels {
		if level != topologyLevels[index] {
			return failedPrecondition("topology_level_mismatch", "observed topology levels do not match the compiled contract")
		}
	}
	return nil
}

// TopologyConstraint is the placement's topology request. At most one of
// Required and Preferred is set, mirroring Kueue's podset annotations: they are
// mutually exclusive there, and expressing both would leave the resulting
// object's behaviour dependent on which annotation the webhook read first.
type TopologyConstraint struct {
	Required  TopologyLevel `json:"required,omitempty"`
	Preferred TopologyLevel `json:"preferred,omitempty"`
}

// UnconstrainedTopology is the zero constraint.
func UnconstrainedTopology() TopologyConstraint { return TopologyConstraint{} }

// RequireTopology asks for all pods within one domain at the given level.
func RequireTopology(level TopologyLevel) TopologyConstraint {
	return TopologyConstraint{Required: level}
}

// PreferTopology asks for one domain at the given level, falling back wider.
func PreferTopology(level TopologyLevel) TopologyConstraint {
	return TopologyConstraint{Preferred: level}
}

func (constraint TopologyConstraint) IsZero() bool {
	return constraint.Required == TopologyLevelNone && constraint.Preferred == TopologyLevelNone
}

func (constraint TopologyConstraint) Validate() error {
	if constraint.Required != TopologyLevelNone && constraint.Preferred != TopologyLevelNone {
		return invalid("topology_constraint_ambiguous", "topology constraint may not be both required and preferred", nil)
	}
	if constraint.Required != TopologyLevelNone && !constraint.Required.Valid() {
		return invalid("topology_required_level_invalid", "required topology level is not one of the two declared levels", nil)
	}
	if constraint.Preferred != TopologyLevelNone && !constraint.Preferred.Valid() {
		return invalid("topology_preferred_level_invalid", "preferred topology level is not one of the two declared levels", nil)
	}
	return nil
}

// Level returns the single level the constraint names, if any.
func (constraint TopologyConstraint) Level() TopologyLevel {
	if constraint.Required != TopologyLevelNone {
		return constraint.Required
	}
	return constraint.Preferred
}

// ValidateForFlavor rejects a topology constraint on a flavor that declares no
// topologyName. The CPU general on-demand flavor has none, so a zone or host
// constraint there is not a tighter placement -- it is an annotation Kueue will
// reject, discovered asynchronously instead of here.
func (constraint TopologyConstraint) ValidateForFlavor(flavor ResourceFlavor) error {
	if err := constraint.Validate(); err != nil {
		return err
	}
	if constraint.IsZero() {
		return nil
	}
	if !flavor.TopologyAware() {
		return denied("topology_flavor_unsupported", "resource flavor declares no topology")
	}
	return nil
}

func (constraint TopologyConstraint) canonical() string {
	return canonicalJoin(string(constraint.Required), string(constraint.Preferred))
}

// Fingerprint binds a constraint to the topology contract it was resolved
// against, so a stored placement can be rejected after a topology replacement
// rather than silently reinterpreted.
func (constraint TopologyConstraint) Fingerprint() identifiers.Digest {
	return identifiers.SHA256String(canonicalJoin(
		"mindclade-topology-constraint-v1",
		TopologyFingerprint().String(),
		constraint.canonical(),
	))
}

// TopologyDomain is one concrete location: a zone, and optionally a host within
// it. A host without a zone is not representable, because the topology is
// ordered and the narrower level is only meaningful inside the wider one.
type TopologyDomain struct {
	Zone string `json:"zone"`
	Host string `json:"host,omitempty"`
}

func (domain TopologyDomain) Validate() error {
	if err := validateName(domain.Zone, "topology_zone"); err != nil {
		return err
	}
	if domain.Host == "" {
		return nil
	}
	return validateName(domain.Host, "topology_host")
}

// Level returns the narrowest level this domain resolves.
func (domain TopologyDomain) Level() TopologyLevel {
	if domain.Host != "" {
		return TopologyLevelHost
	}
	return TopologyLevelZone
}

func (domain TopologyDomain) canonical() string {
	return canonicalJoin(domain.Zone, domain.Host)
}

// TopologyAssignment is the bounded set of domains a bound placement actually
// landed on, recorded so a later reconcile can tell whether the constraint was
// honoured without re-reading the cluster.
type TopologyAssignment struct {
	Constraint TopologyConstraint `json:"constraint"`
	Domains    []TopologyDomain   `json:"domains,omitempty"`
}

func (assignment TopologyAssignment) IsZero() bool {
	return assignment.Constraint.IsZero() && len(assignment.Domains) == 0
}

func (assignment TopologyAssignment) Validate() error {
	if err := assignment.Constraint.Validate(); err != nil {
		return err
	}
	if len(assignment.Domains) > MaximumTopologyDomains {
		return exhausted("topology_assignment_bound", "topology assignment exceeds the domain bound")
	}
	seen := make(map[string]struct{}, len(assignment.Domains))
	for _, domain := range assignment.Domains {
		if err := domain.Validate(); err != nil {
			return err
		}
		key := domain.canonical()
		if _, exists := seen[key]; exists {
			return invalid("topology_assignment_duplicate", "topology assignment repeats a domain", nil)
		}
		seen[key] = struct{}{}
	}
	if assignment.Constraint.Required == TopologyLevelZone && len(assignment.Domains) > 1 {
		return failedPrecondition("topology_required_zone_violated", "a zone-required placement spans more than one zone")
	}
	if assignment.Constraint.Required == TopologyLevelHost {
		for _, domain := range assignment.Domains {
			if domain.Host == "" {
				return failedPrecondition("topology_required_host_violated", "a host-required placement recorded a zone-only domain")
			}
		}
		if len(assignment.Domains) > 1 {
			return failedPrecondition("topology_required_host_violated", "a host-required placement spans more than one host")
		}
	}
	return nil
}

func (assignment TopologyAssignment) Clone() TopologyAssignment {
	assignment.Domains = append([]TopologyDomain(nil), assignment.Domains...)
	return assignment
}

func (assignment TopologyAssignment) canonical() string {
	values := make([]string, 0, len(assignment.Domains)+2)
	values = append(values, assignment.Constraint.canonical(), strconv.Itoa(len(assignment.Domains)))
	for _, domain := range assignment.Domains {
		values = append(values, domain.canonical())
	}
	return canonicalJoin(values...)
}
