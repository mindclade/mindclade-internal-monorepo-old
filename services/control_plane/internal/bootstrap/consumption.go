// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package bootstrap

import (
	_ "embed"
	"encoding/json"
	"sync"

	"go.mindclade.dev/libs/go/faults"
)

// consumptionSchemaVersion must match the version written by
// tools/analysis/check_foundation_consumption.py.
const consumptionSchemaVersion = 1

// consumptionDocument is generated from the repository's Go import graph, not
// written by hand. A hand-maintained inventory of package names cannot fail:
// the previous one claimed roles consumed PostgreSQL adapters that no Go file
// imported, and nothing compared the claim with the build. Presubmit
// regenerates this document and fails on any difference.
//
//go:embed consumption.json
var consumptionDocument []byte

// Consumption is qualification metadata describing which reusable Go packages
// one process role links, directly or through its composition root. It is not
// a dependency-injection container or domain abstraction.
type Consumption struct {
	Role     Role
	Packages []string
}

type consumptionSchema struct {
	SchemaVersion int                 `json:"schema_version"`
	Roles         map[string][]string `json:"roles"`
}

var loadConsumption = sync.OnceValues(func() (map[Role][]string, error) {
	var document consumptionSchema
	if err := json.Unmarshal(consumptionDocument, &document); err != nil {
		return nil, faults.Wrap(err, faults.CodeInternal,
			"unreadable control-plane consumption document",
			faults.WithReason("invalid_consumption_document"),
			faults.WithOperation("controlplane.bootstrap.loadConsumption"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	if document.SchemaVersion != consumptionSchemaVersion {
		return nil, faults.New(
			faults.CodeInternal,
			"unsupported control-plane consumption schema",
			faults.WithReason("unsupported_consumption_schema"),
			faults.WithOperation("controlplane.bootstrap.loadConsumption"),
			faults.WithField("schema_version", document.SchemaVersion),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	roles := make(map[Role][]string, len(document.Roles))
	for role, packages := range document.Roles {
		roles[Role(role)] = packages
	}
	return roles, nil
})

// ConsumptionFor returns an isolated package inventory for role. The document
// is generated in sorted order, so no sorting happens here.
func ConsumptionFor(role Role) (Consumption, error) {
	roles, err := loadConsumption()
	if err != nil {
		return Consumption{}, err
	}
	packages, ok := roles[role]
	if !ok {
		return Consumption{}, faults.New(
			faults.CodeInvalidArgument,
			"unsupported control-plane consumption role",
			faults.WithReason("unsupported_consumption_role"),
			faults.WithOperation("controlplane.bootstrap.ConsumptionFor"),
			faults.WithField("role", role.String()),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	return Consumption{Role: role, Packages: append([]string(nil), packages...)}, nil
}

// ConsumptionMatrix returns all role inventories in stable profile order.
func ConsumptionMatrix() []Consumption {
	profiles := Profiles()
	result := make([]Consumption, 0, len(profiles))
	for _, profile := range profiles {
		consumption, err := ConsumptionFor(profile.Role)
		if err == nil {
			result = append(result, consumption)
		}
	}
	return result
}
