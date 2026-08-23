// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package scheduling

import (
	"math/bits"
	"sort"
)

// PriorityClass is a Kubernetes PriorityClass declared by
// infra/kubernetes/base/priority-classes.yaml. There are two, and there is no
// third: a scheduler that invents a priority name produces pods the API server
// admits with no priority at all, which silently reorders the entire fleet.
type PriorityClass string

const (
	// PriorityPlatformCritical is value 1000000, preemptionPolicy
	// PreemptLowerPriority. Tier-1 control-plane work.
	PriorityPlatformCritical PriorityClass = "mindclade-platform-critical"
	// PriorityBatch is value 10000, preemptionPolicy Never. Interruptible work
	// that must not preempt serving or platform work -- the Never is enforced
	// by the cluster, and MayPreempt below mirrors it so this package refuses
	// to plan an eviction the cluster would not perform.
	PriorityBatch PriorityClass = "mindclade-batch"
)

const (
	platformCriticalValue int32 = 1_000_000
	batchValue            int32 = 10_000

	// PriorityShareBand is how far fair share may move an item within its
	// class. It is far smaller than the 990000 gap between the two class
	// values, which is the property that matters: no amount of fair-share
	// penalty can push a batch item above a platform-critical one. Without
	// that bound, "fairness" would quietly become a preemption channel for a
	// class whose preemptionPolicy is Never.
	PriorityShareBand = int32(9_000)

	// MaximumShareClaims bounds one fair-share evaluation. The claim list is
	// walked and sorted on every admission decision, so it needs a ceiling.
	MaximumShareClaims = 4_096

	// MaximumShareWeight bounds a single tenant weight so the cross-multiplied
	// comparison below cannot overflow: ShareScale (2^20) times MaximumShareWeight
	// (2^32) stays far inside uint64.
	MaximumShareWeight = uint32(1) << 20
)

var priorityClasses = [...]PriorityClass{PriorityPlatformCritical, PriorityBatch}

// PriorityClasses returns the two classes, highest first.
func PriorityClasses() []PriorityClass {
	return append([]PriorityClass(nil), priorityClasses[:]...)
}

func (class PriorityClass) String() string { return string(class) }

func (class PriorityClass) Valid() bool {
	return class == PriorityPlatformCritical || class == PriorityBatch
}

// Value returns the PriorityClass value as declared in the cluster.
func (class PriorityClass) Value() int32 {
	switch class {
	case PriorityPlatformCritical:
		return platformCriticalValue
	case PriorityBatch:
		return batchValue
	default:
		return 0
	}
}

// MayPreempt mirrors the cluster's preemptionPolicy. Batch is Never, so Go-side
// preemption policy must refuse to select victims on its behalf; planning an
// eviction the cluster will not honour produces an evicted victim and a
// candidate that still does not run.
func (class PriorityClass) MayPreempt() bool { return class == PriorityPlatformCritical }

// Preempts reports whether this class outranks another. Kubernetes preempts
// strictly lower priority only, so equal values never displace each other.
func (class PriorityClass) Preempts(other PriorityClass) bool {
	if !class.Valid() || !other.Valid() || !class.MayPreempt() {
		return false
	}
	return class.Value() > other.Value()
}

// QueuePriority projects a class and a fair-share position onto
// workqueue.Item.Priority. The work queue claims in descending priority order,
// so a larger number is claimed sooner; a tenant already holding a large share
// of the fleet is penalised within its class, never across classes.
//
// share is parts per million, as produced by FairShare.Position.
func QueuePriority(class PriorityClass, share uint64) (int32, error) {
	if !class.Valid() {
		return 0, invalid("priority_class_invalid", "priority class is not one of the two declared classes", nil)
	}
	if share > ShareScale {
		return 0, invalid("fair_share_out_of_range", "fair share position is outside bounds", nil)
	}
	penalty := int32(share * uint64(PriorityShareBand) / ShareScale)
	return class.Value() - penalty, nil
}

// ShareClaim is one tenant's weight and current usage in a capacity domain.
type ShareClaim struct {
	Tenant string `json:"tenant"`
	Weight uint32 `json:"weight"`
	Used   Demand `json:"used"`
}

func (claim ShareClaim) Validate(domain CapacityDomain) error {
	if err := validateName(claim.Tenant, "tenant"); err != nil {
		return err
	}
	if claim.Weight == 0 || claim.Weight > MaximumShareWeight {
		return invalid("share_weight_out_of_range", "fair-share weight is outside bounds", nil)
	}
	return claim.Used.ValidateForDomain(domain, false)
}

func (claim ShareClaim) clone() ShareClaim {
	claim.Used = claim.Used.Clone()
	return claim
}

// FairShare is the weighted dominant-resource-share view of one capacity
// domain.
//
// Dominant resource share rather than a per-resource comparison, because the
// five covered resources are one ResourceGroup and are not commensurable: a
// tenant holding 80% of the GPUs and 2% of the memory is using 80% of the
// domain, and summing the dimensions would say otherwise. All arithmetic is
// integer, and every comparison is by cross-multiplication rather than
// division, so two replicas evaluating the same snapshot produce identical
// orderings.
type FairShare struct {
	Domain   CapacityDomain `json:"domain"`
	Capacity Demand         `json:"capacity"`
	Claims   []ShareClaim   `json:"claims,omitempty"`
}

func (share FairShare) Validate() error {
	if err := share.Domain.Validate(); err != nil {
		return err
	}
	if err := share.Capacity.ValidateForDomain(share.Domain, false); err != nil {
		return err
	}
	if len(share.Claims) > MaximumShareClaims {
		return exhausted("fair_share_claim_bound", "fair-share claim list exceeds its bound")
	}
	seen := make(map[string]struct{}, len(share.Claims))
	for _, claim := range share.Claims {
		if err := claim.Validate(share.Domain); err != nil {
			return err
		}
		if _, exists := seen[claim.Tenant]; exists {
			return invalid("fair_share_duplicate_tenant", "fair-share claims repeat a tenant", nil)
		}
		seen[claim.Tenant] = struct{}{}
	}
	return nil
}

func (share FairShare) Clone() FairShare {
	share.Capacity = share.Capacity.Clone()
	claims := make([]ShareClaim, 0, len(share.Claims))
	for _, claim := range share.Claims {
		claims = append(claims, claim.clone())
	}
	share.Claims = claims
	return share
}

// TotalWeight returns the summed weight of every claim.
func (share FairShare) TotalWeight() uint64 {
	total := uint64(0)
	for _, claim := range share.Claims {
		total += uint64(claim.Weight)
	}
	return total
}

// Position returns a tenant's weighted dominant share in parts per million,
// saturating at ShareScale.
//
// The weighting divides the raw dominant share by the tenant's weight fraction,
// so a tenant with twice the weight has to hold twice the capacity before it
// ranks equally. A tenant with no claim has no usage and no weight, and is
// treated as entirely unserved -- position zero -- which is what puts brand-new
// tenants at the front of the order rather than the back.
func (share FairShare) Position(tenant string) (uint64, error) {
	if err := share.Validate(); err != nil {
		return 0, err
	}
	total := share.TotalWeight()
	for _, claim := range share.Claims {
		if claim.Tenant != tenant {
			continue
		}
		raw := claim.Used.DominantShare(share.Capacity)
		if total == 0 || claim.Weight == 0 {
			return raw, nil
		}
		// raw * total / weight, in 128 bits so a large tenant count cannot wrap.
		high, low := bits.Mul64(raw, total)
		divisor := uint64(claim.Weight)
		if high >= divisor {
			return ShareScale, nil
		}
		weighted, _ := bits.Div64(high, low, divisor)
		if weighted > ShareScale {
			return ShareScale, nil
		}
		return weighted, nil
	}
	return 0, nil
}

// Entitlement returns the capacity a tenant is owed by weight: each covered
// resource scaled by the tenant's weight fraction, floored.
//
// Flooring is deliberate. Rounding up would let the summed entitlements exceed
// the nominal quota, and a fairness calculation that over-promises is worse
// than one that leaves a remainder unassigned -- the remainder is still
// schedulable by whoever is furthest below their share.
func (share FairShare) Entitlement(tenant string) (Demand, error) {
	if err := share.Validate(); err != nil {
		return nil, err
	}
	total := share.TotalWeight()
	if total == 0 {
		return nil, failedPrecondition("fair_share_unweighted", "fair share has no weighted claims")
	}
	weight := uint64(0)
	for _, claim := range share.Claims {
		if claim.Tenant == tenant {
			weight = uint64(claim.Weight)
			break
		}
	}
	entitlement := make(Demand, len(coveredResources))
	if weight == 0 {
		return entitlement, nil
	}
	for _, name := range coveredResources {
		capacity := share.Capacity[name]
		if capacity == 0 {
			continue
		}
		high, low := bits.Mul64(capacity, weight)
		if high >= total {
			return nil, invalid("fair_share_entitlement_overflow", "fair-share entitlement is outside bounds", nil)
		}
		amount, _ := bits.Div64(high, low, total)
		entitlement[name] = amount
	}
	return entitlement, nil
}

// Order returns tenants least-served first: ascending weighted dominant share,
// ties broken by tenant name.
//
// The tie-break is not cosmetic. Two replicas evaluating the same snapshot must
// pick the same tenant, or a leadership handover reorders the queue and the
// tenant that was about to be served loses its turn every time leadership moves.
func (share FairShare) Order() ([]string, error) {
	if err := share.Validate(); err != nil {
		return nil, err
	}
	type ranked struct {
		tenant string
		used   uint64
		weight uint64
	}
	entries := make([]ranked, 0, len(share.Claims))
	for _, claim := range share.Claims {
		entries = append(entries, ranked{
			tenant: claim.Tenant,
			used:   claim.Used.DominantShare(share.Capacity),
			weight: uint64(claim.Weight),
		})
	}
	sort.Slice(entries, func(left, right int) bool {
		// used[l]/weight[l] < used[r]/weight[r], cross-multiplied. Both sides
		// are bounded by ShareScale * MaximumShareWeight, well inside uint64.
		leftShare := entries[left].used * entries[right].weight
		rightShare := entries[right].used * entries[left].weight
		if leftShare != rightShare {
			return leftShare < rightShare
		}
		return entries[left].tenant < entries[right].tenant
	})
	order := make([]string, 0, len(entries))
	for _, entry := range entries {
		order = append(order, entry.tenant)
	}
	return order, nil
}

// Admits reports whether one tenant may take an additional demand without
// exceeding its weighted entitlement. A tenant already at or above its share is
// not refused outright -- it is refused only while another tenant is below its
// own share, which is what BelowShare answers.
func (share FairShare) Admits(tenant string, demand Demand) (bool, error) {
	if err := share.Validate(); err != nil {
		return false, err
	}
	// With no weighted claims there is nothing to divide, so fairness
	// constrains nothing. This is not a fail-open: capacity is still bounded by
	// the ledger, which is checked separately and first.
	if share.TotalWeight() == 0 {
		return true, nil
	}
	entitlement, err := share.Entitlement(tenant)
	if err != nil {
		return false, err
	}
	used := Demand(nil)
	for _, claim := range share.Claims {
		if claim.Tenant == tenant {
			used = claim.Used
			break
		}
	}
	projected, err := used.add(demand)
	if err != nil {
		return false, err
	}
	return projected.Fits(entitlement), nil
}

// Contended reports whether any other tenant is below its weighted share. When
// nothing is contended, a tenant over its entitlement may still use idle
// capacity; when something is, it may not.
func (share FairShare) Contended(tenant string) (bool, error) {
	if err := share.Validate(); err != nil {
		return false, err
	}
	for _, claim := range share.Claims {
		if claim.Tenant == tenant {
			continue
		}
		entitlement, err := share.Entitlement(claim.Tenant)
		if err != nil {
			return false, err
		}
		// Fits reads "entitlement is within used", i.e. the tenant has taken at
		// least its share in every dimension. Its negation is the contended case.
		if !entitlement.Fits(claim.Used) {
			return true, nil
		}
	}
	return false, nil
}

func (share FairShare) canonical() string {
	values := make([]string, 0, 2*len(share.Claims)+2)
	values = append(values, share.Domain.String(), share.Capacity.canonical())
	ordered := append([]ShareClaim(nil), share.Claims...)
	sort.Slice(ordered, func(left, right int) bool { return ordered[left].Tenant < ordered[right].Tenant })
	for _, claim := range ordered {
		values = append(values, claim.Tenant, claim.Used.canonical())
	}
	return canonicalJoin(values...)
}
