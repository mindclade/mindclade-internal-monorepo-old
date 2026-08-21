// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package lineage

// Relation describes the immutable derivation direction From -> To.
type Relation string

const (
	RelationConsumedBy  Relation = "consumed_by"
	RelationProduced    Relation = "produced"
	RelationDerived     Relation = "derived"
	RelationResumed     Relation = "resumed"
	RelationEvaluatedBy Relation = "evaluated_by"
	RelationPackagedAs  Relation = "packaged_as"
	RelationDeployedAs  Relation = "deployed_as"
)

func (relation Relation) Valid() bool {
	switch relation {
	case RelationConsumedBy, RelationProduced, RelationDerived, RelationResumed,
		RelationEvaluatedBy, RelationPackagedAs, RelationDeployedAs:
		return true
	default:
		return false
	}
}

// Edge links two graph-local node IDs. It never carries a mutable path or alias.
type Edge struct {
	From     string   `json:"from"`
	To       string   `json:"to"`
	Relation Relation `json:"relation"`
}
