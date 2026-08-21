// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package admission

import (
	"math"
	"strconv"
	"strings"
)

// Unit is an integer-only budget dimension. Cost uses micros to avoid floating-point drift.
type Unit string

const (
	UnitRequests     Unit = "requests"
	UnitInputTokens  Unit = "input_tokens"
	UnitOutputTokens Unit = "output_tokens"
	UnitCostMicros   Unit = "cost_micros"
	MaximumAmount         = uint64(1_000_000_000_000_000_000)
)

var units = [...]Unit{UnitRequests, UnitInputTokens, UnitOutputTokens, UnitCostMicros}

// Quota is a bounded amount for each supported dimension. Missing dimensions are zero.
type Quota map[Unit]uint64

func (q Quota) Validate(requirePositive bool) error {
	positive := false
	for unit, amount := range q {
		if !unit.Valid() {
			return invalid("quota_unit_invalid", "quota contains an unsupported unit", nil)
		}
		if amount > MaximumAmount {
			return invalid("quota_amount_out_of_range", "quota amount is outside bounds", nil)
		}
		positive = positive || amount > 0
	}
	if requirePositive && !positive {
		return invalid("quota_empty", "quota must contain a positive amount", nil)
	}
	return nil
}

func (unit Unit) Valid() bool {
	for _, candidate := range units {
		if unit == candidate {
			return true
		}
	}
	return false
}

func (q Quota) Clone() Quota {
	clone := make(Quota, len(q))
	for unit, amount := range q {
		clone[unit] = amount
	}
	return clone
}

// Fits reports whether every amount in q is less than or equal to the matching limit.
func (q Quota) Fits(limit Quota) bool {
	for _, unit := range units {
		if q[unit] > limit[unit] {
			return false
		}
	}
	return true
}

func (q Quota) Equal(other Quota) bool {
	for _, unit := range units {
		if q[unit] != other[unit] {
			return false
		}
	}
	return true
}

func (q Quota) add(other Quota) (Quota, error) {
	result := q.Clone()
	for _, unit := range units {
		if math.MaxUint64-result[unit] < other[unit] || result[unit]+other[unit] > MaximumAmount {
			return nil, invalid("quota_sum_overflow", "quota sum is outside bounds", nil)
		}
		result[unit] += other[unit]
	}
	return result, nil
}

func (q Quota) canonical() string {
	var builder strings.Builder
	for index, unit := range units {
		if index > 0 {
			builder.WriteByte(',')
		}
		builder.WriteString(string(unit))
		builder.WriteByte('=')
		builder.WriteString(strconv.FormatUint(q[unit], 10))
	}
	return builder.String()
}
