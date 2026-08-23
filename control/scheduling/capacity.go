// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package scheduling

import (
	"math"
	"math/bits"
	"strconv"
	"strings"
)

// ResourceName is one covered resource of the single Kueue ResourceGroup.
//
// All five live in ONE group in infra/kubernetes/platform/kueue/resources.yaml,
// which is a hard modelling constraint rather than a layout detail: a workload
// receives its flavor selectors for every covered resource from one flavor, so
// it can never take CPU from the general on-demand flavor and GPU from an H100
// flavor. Demand is therefore a single coherent vector, never a per-resource
// bag that could be satisfied piecewise.
type ResourceName string

const (
	// ResourceCPU is counted in millicores. Kubernetes' own canonical integer
	// unit for CPU; using cores would force fractions, and this package is
	// integer-only so that two schedulers replaying the same ledger agree.
	ResourceCPU ResourceName = "cpu"
	// ResourceMemory is counted in bytes.
	ResourceMemory ResourceName = "memory"
	// ResourceEphemeralStorage is counted in bytes.
	ResourceEphemeralStorage ResourceName = "ephemeral-storage"
	// ResourceGPU is counted in whole devices. Fractional GPUs are not a thing
	// the cluster grants: nvidia.com/gpu is an integer extended resource.
	ResourceGPU ResourceName = "nvidia.com/gpu"
	// ResourcePods is counted in pods.
	ResourcePods ResourceName = "pods"
)

// coveredResources is the ResourceGroup, in the order the manifest lists it.
// Every canonical form and every comparison walks this array rather than
// ranging over a map, so results do not depend on map iteration order.
var coveredResources = [...]ResourceName{
	ResourceCPU,
	ResourceMemory,
	ResourceEphemeralStorage,
	ResourceGPU,
	ResourcePods,
}

// MaximumResourceAmount bounds every individual amount. It is far above any
// plausible fleet -- 2^56 bytes is 72 petabytes -- and far below the point
// where a sum could wrap, which is the property that matters: every addition
// below can be checked against it without needing 128-bit arithmetic.
const MaximumResourceAmount = uint64(1) << 56

// ShareScale expresses fair-share ratios in parts per million. Integer scaling
// rather than a float ratio, because fair-share ordering has to be identical on
// every replica that evaluates it and float rounding is not something a
// distributed comparison can rely on.
const ShareScale = uint64(1_000_000)

func (name ResourceName) String() string { return string(name) }

func (name ResourceName) Valid() bool {
	for _, candidate := range coveredResources {
		if name == candidate {
			return true
		}
	}
	return false
}

// CoveredResources returns the resources this domain's ClusterQueue actually
// grants. The batch-cpu queue omits nvidia.com/gpu from its coveredResources,
// so a GPU demand there is not a shortage that more nodes would fix -- the
// queue has no dimension to charge it against, and Kueue will never admit it.
func (domain CapacityDomain) CoveredResources() []ResourceName {
	if domain.Accelerator() == AcceleratorNone {
		return []ResourceName{ResourceCPU, ResourceMemory, ResourceEphemeralStorage, ResourcePods}
	}
	return append([]ResourceName(nil), coveredResources[:]...)
}

// Covers reports whether this domain's ResourceGroup includes one resource.
func (domain CapacityDomain) Covers(name ResourceName) bool {
	for _, candidate := range domain.CoveredResources() {
		if candidate == name {
			return true
		}
	}
	return false
}

// Demand is a bounded amount for each covered resource. An absent key is zero;
// the zero value of the map type is a valid empty demand.
type Demand map[ResourceName]uint64

// Validate checks every amount against the covered set and the bound.
// requirePositive rejects an all-zero demand, which is what a placement request
// must never be: a workload that asks for nothing still occupies a pod slot,
// and admitting it without charging anything makes the ledger lie.
func (demand Demand) Validate(requirePositive bool) error {
	positive := false
	for name, amount := range demand {
		if !name.Valid() {
			return invalid("demand_resource_invalid", "demand names a resource outside the covered group", nil)
		}
		if amount > MaximumResourceAmount {
			return invalid("demand_amount_out_of_range", "demand amount is outside bounds", nil)
		}
		positive = positive || amount > 0
	}
	if requirePositive && !positive {
		return invalid("demand_empty", "demand must contain a positive amount", nil)
	}
	return nil
}

// ValidateForDomain additionally rejects a positive amount for a resource the
// domain's ResourceGroup does not cover.
func (demand Demand) ValidateForDomain(domain CapacityDomain, requirePositive bool) error {
	if err := domain.Validate(); err != nil {
		return err
	}
	if err := demand.Validate(requirePositive); err != nil {
		return err
	}
	for _, name := range coveredResources {
		if demand[name] > 0 && !domain.Covers(name) {
			return denied("demand_resource_uncovered", "capacity domain does not cover a requested resource")
		}
	}
	return nil
}

func (demand Demand) Clone() Demand {
	if demand == nil {
		return nil
	}
	clone := make(Demand, len(demand))
	for name, amount := range demand {
		clone[name] = amount
	}
	return clone
}

// Fits reports whether every amount is within the matching limit. The whole
// vector must fit; there is no partial satisfaction, because the ResourceGroup
// is admitted or refused as a unit.
func (demand Demand) Fits(limit Demand) bool {
	for _, name := range coveredResources {
		if demand[name] > limit[name] {
			return false
		}
	}
	return true
}

func (demand Demand) Equal(other Demand) bool {
	for _, name := range coveredResources {
		if demand[name] != other[name] {
			return false
		}
	}
	return true
}

// IsZero reports whether every covered amount is zero.
func (demand Demand) IsZero() bool {
	for _, name := range coveredResources {
		if demand[name] != 0 {
			return false
		}
	}
	return true
}

func (demand Demand) add(other Demand) (Demand, error) {
	result := make(Demand, len(coveredResources))
	for _, name := range coveredResources {
		left, right := demand[name], other[name]
		if math.MaxUint64-left < right || left+right > MaximumResourceAmount {
			return nil, invalid("demand_sum_overflow", "demand sum is outside bounds", nil)
		}
		result[name] = left + right
	}
	return result, nil
}

// sub is saturation-free on purpose. A release that would drive a ledger
// negative means the ledger and the reservation disagree about what was held,
// and clamping at zero would hide the divergence while permanently leaking the
// difference in the other direction.
func (demand Demand) sub(other Demand) (Demand, error) {
	result := make(Demand, len(coveredResources))
	for _, name := range coveredResources {
		left, right := demand[name], other[name]
		if left < right {
			return nil, conflict("demand_underflow", "released demand exceeds the recorded reservation")
		}
		result[name] = left - right
	}
	return result, nil
}

func (demand Demand) scale(factor uint64) (Demand, error) {
	result := make(Demand, len(coveredResources))
	for _, name := range coveredResources {
		amount := demand[name]
		if amount != 0 && factor > MaximumResourceAmount/amount {
			return nil, invalid("demand_scale_overflow", "scaled demand is outside bounds", nil)
		}
		result[name] = amount * factor
	}
	return result, nil
}

// Scale multiplies every amount by a replica count. Gang semantics are not
// implied: nothing in the cluster contract specifies gang scheduling, so this
// is only the arithmetic a caller needs when one placement covers N identical
// pods and the queue charges for all of them.
func (demand Demand) Scale(factor uint64) (Demand, error) {
	if factor == 0 {
		return nil, invalid("demand_scale_invalid", "scale factor must be positive", nil)
	}
	if err := demand.Validate(false); err != nil {
		return nil, err
	}
	return demand.scale(factor)
}

func (demand Demand) canonical() string {
	var builder strings.Builder
	for index, name := range coveredResources {
		if index > 0 {
			builder.WriteByte(',')
		}
		builder.WriteString(string(name))
		builder.WriteByte('=')
		builder.WriteString(strconv.FormatUint(demand[name], 10))
	}
	return builder.String()
}

// DominantShare returns the largest per-resource share of capacity this demand
// occupies, in parts per million, saturating at ShareScale when the demand
// exceeds capacity in any dimension.
//
// Dominant-resource share is what makes fairness comparable across pools that
// consume different things: a GPU job and a high-memory search job cannot be
// ranked by summing incomparable units, but they can be ranked by how much of
// the scarcest thing each one takes. A resource with zero capacity contributes
// nothing rather than dividing by zero -- an uncovered dimension is not a
// dimension this domain competes on.
func (demand Demand) DominantShare(capacity Demand) uint64 {
	largest := uint64(0)
	for _, name := range coveredResources {
		limit := capacity[name]
		if limit == 0 {
			continue
		}
		used := demand[name]
		if used >= limit {
			return ShareScale
		}
		high, low := bits.Mul64(used, ShareScale)
		if high >= limit {
			return ShareScale
		}
		share, _ := bits.Div64(high, low, limit)
		if share > largest {
			largest = share
		}
	}
	if largest > ShareScale {
		return ShareScale
	}
	return largest
}

// Allocatable is one capacity domain's ledger: the nominal quota its single
// flavor grants and the amount currently held by live reservations.
//
// Nominal is deliberately not defaulted. Every ClusterQueue in the manifest
// ships with nominalQuota "0" and stopPolicy Hold, and inventing a non-zero
// default here would let this package admit work the cluster is holding back.
// Zero quota means nothing is admitted, which is the correct answer today.
type Allocatable struct {
	Domain   CapacityDomain `json:"domain"`
	Nominal  Demand         `json:"nominal"`
	Reserved Demand         `json:"reserved"`
}

func (allocatable Allocatable) Validate() error {
	if err := allocatable.Domain.Validate(); err != nil {
		return err
	}
	if err := allocatable.Nominal.ValidateForDomain(allocatable.Domain, false); err != nil {
		return err
	}
	if err := allocatable.Reserved.ValidateForDomain(allocatable.Domain, false); err != nil {
		return err
	}
	if !allocatable.Reserved.Fits(allocatable.Nominal) {
		return conflict("allocatable_over_reserved", "reserved capacity exceeds the nominal quota")
	}
	return nil
}

func (allocatable Allocatable) Clone() Allocatable {
	allocatable.Nominal = allocatable.Nominal.Clone()
	allocatable.Reserved = allocatable.Reserved.Clone()
	return allocatable
}

// Available returns the unreserved remainder of the nominal quota.
func (allocatable Allocatable) Available() (Demand, error) {
	if err := allocatable.Validate(); err != nil {
		return nil, err
	}
	return allocatable.Nominal.sub(allocatable.Reserved)
}

// Admits reports whether one more demand fits without exceeding the quota.
// It answers false rather than erroring on an over-large demand: not fitting is
// an ordinary scheduling outcome, not a malformed request.
func (allocatable Allocatable) Admits(demand Demand) (bool, error) {
	if err := demand.ValidateForDomain(allocatable.Domain, false); err != nil {
		return false, err
	}
	available, err := allocatable.Available()
	if err != nil {
		return false, err
	}
	return demand.Fits(available), nil
}

// Reserve returns the next ledger with demand held. It fails closed on a
// shortfall so a caller cannot accidentally over-commit by ignoring a boolean.
func (allocatable Allocatable) Reserve(demand Demand) (Allocatable, error) {
	admits, err := allocatable.Admits(demand)
	if err != nil {
		return Allocatable{}, err
	}
	if !admits {
		return Allocatable{}, exhausted("capacity_exhausted", "capacity domain cannot satisfy the demand")
	}
	reserved, err := allocatable.Reserved.add(demand)
	if err != nil {
		return Allocatable{}, err
	}
	next := allocatable.Clone()
	next.Reserved = reserved
	if err := next.Validate(); err != nil {
		return Allocatable{}, err
	}
	return next, nil
}

// Free returns the next ledger with demand released.
func (allocatable Allocatable) Free(demand Demand) (Allocatable, error) {
	if err := allocatable.Validate(); err != nil {
		return Allocatable{}, err
	}
	if err := demand.ValidateForDomain(allocatable.Domain, false); err != nil {
		return Allocatable{}, err
	}
	reserved, err := allocatable.Reserved.sub(demand)
	if err != nil {
		return Allocatable{}, err
	}
	next := allocatable.Clone()
	next.Reserved = reserved
	if err := next.Validate(); err != nil {
		return Allocatable{}, err
	}
	return next, nil
}

// Utilization returns the dominant share of this domain currently held.
func (allocatable Allocatable) Utilization() uint64 {
	return allocatable.Reserved.DominantShare(allocatable.Nominal)
}

func (allocatable Allocatable) canonical() string {
	return canonicalJoin(allocatable.Domain.String(), allocatable.Nominal.canonical(), allocatable.Reserved.canonical())
}
