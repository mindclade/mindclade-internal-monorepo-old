// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package bootstrap

import (
	"sort"

	"go.mindclade.dev/libs/go/faults"
)

// Consumption is qualification metadata describing which reusable Go packages
// a process role is expected to consume directly or through its composition
// root. It is not a dependency-injection container or domain abstraction.
type Consumption struct {
	Role     Role
	Packages []string
}

var commonConsumption = []string{
	"libs/go/clock",
	"libs/go/config",
	"libs/go/faults",
	"libs/go/identifiers",
	"libs/go/observability",
	"libs/go/requestmeta",
	"libs/go/retry",
	"libs/go/servicekit",
	"libs/go/servicekit/production",
	"libs/go/storage/sql/postgres",
	"libs/go/storage/sql/transaction",
}

var roleConsumption = map[Role][]string{
	RoleAPI: {
		"libs/go/audit", "libs/go/audit/postgres", "libs/go/auth",
		"libs/go/connectx", "libs/go/grpcx", "libs/go/httpx",
		"libs/go/idempotency", "libs/go/idempotency/postgres",
		"libs/go/pagination", "libs/go/resourceversion", "libs/go/signing",
		"libs/go/coordination/outbox", "libs/go/coordination/outbox/postgres",
		"libs/go/storage/blob", "libs/go/storage/cache", "libs/go/storage/sql",
	},
	RoleAdmin: {
		"libs/go/audit", "libs/go/audit/postgres", "libs/go/auth",
		"libs/go/connectx", "libs/go/grpcx", "libs/go/httpx",
		"libs/go/idempotency", "libs/go/idempotency/postgres",
		"libs/go/pagination", "libs/go/resourceversion", "libs/go/signing",
		"libs/go/coordination/outbox", "libs/go/coordination/outbox/postgres",
		"libs/go/storage/sql",
	},
	RoleRegistry: {
		"libs/go/audit", "libs/go/audit/postgres", "libs/go/auth",
		"libs/go/connectx", "libs/go/grpcx", "libs/go/httpx",
		"libs/go/idempotency", "libs/go/idempotency/postgres",
		"libs/go/pagination", "libs/go/resourceversion", "libs/go/signing",
		"libs/go/coordination/outbox", "libs/go/coordination/outbox/postgres",
		"libs/go/storage/blob", "libs/go/storage/cache", "libs/go/storage/sql",
	},
	RoleScheduler: {
		"libs/go/audit", "libs/go/audit/postgres", "libs/go/idempotency", "libs/go/idempotency/postgres",
		"libs/go/kubernetes", "libs/go/messaging", "libs/go/messaging/pubsub", "libs/go/resourceversion", "libs/go/signing",
		"libs/go/storage/lease", "libs/go/storage/lease/postgres",
		"libs/go/coordination/leadership", "libs/go/coordination/outbox", "libs/go/coordination/outbox/postgres",
		"libs/go/coordination/workqueue", "libs/go/coordination/workqueue/postgres",
		"libs/go/storage/sql",
	},
	RoleController: {
		"libs/go/audit", "libs/go/audit/postgres", "libs/go/idempotency", "libs/go/idempotency/postgres",
		"libs/go/kubernetes", "libs/go/messaging", "libs/go/messaging/pubsub", "libs/go/resourceversion",
		"libs/go/storage/lease", "libs/go/storage/lease/postgres",
		"libs/go/coordination/leadership", "libs/go/coordination/outbox", "libs/go/coordination/outbox/postgres",
		"libs/go/coordination/workqueue", "libs/go/coordination/workqueue/postgres",
		"libs/go/storage/sql",
	},
	RoleOperator: {
		"libs/go/audit", "libs/go/audit/postgres", "libs/go/idempotency", "libs/go/idempotency/postgres",
		"libs/go/kubernetes", "libs/go/messaging", "libs/go/messaging/pubsub", "libs/go/resourceversion",
		"libs/go/storage/lease", "libs/go/storage/lease/postgres",
		"libs/go/coordination/leadership", "libs/go/coordination/outbox", "libs/go/coordination/outbox/postgres",
		"libs/go/coordination/workqueue", "libs/go/coordination/workqueue/postgres",
		"libs/go/storage/sql",
	},
	RoleIngestionController: {
		"libs/go/audit", "libs/go/audit/postgres", "libs/go/idempotency", "libs/go/idempotency/postgres",
		"libs/go/kubernetes", "libs/go/messaging", "libs/go/messaging/pubsub", "libs/go/resourceversion",
		"libs/go/storage/blob", "libs/go/storage/cache", "libs/go/storage/lease", "libs/go/storage/lease/postgres",
		"libs/go/coordination/cursor", "libs/go/coordination/cursor/postgres",
		"libs/go/coordination/leadership", "libs/go/coordination/outbox", "libs/go/coordination/outbox/postgres",
		"libs/go/coordination/workqueue", "libs/go/coordination/workqueue/postgres",
		"libs/go/storage/sql",
	},
	RoleEventProjector: {
		"libs/go/idempotency", "libs/go/idempotency/postgres", "libs/go/messaging", "libs/go/messaging/pubsub",
		"libs/go/storage/lease", "libs/go/storage/lease/postgres",
		"libs/go/coordination/cursor", "libs/go/coordination/cursor/postgres",
		"libs/go/coordination/inbox", "libs/go/coordination/leadership", "libs/go/coordination/projector",
		"libs/go/storage/sql",
	},
	RoleEventDispatcher: {
		"libs/go/messaging", "libs/go/messaging/pubsub", "libs/go/storage/lease", "libs/go/storage/lease/postgres",
		"libs/go/coordination/outbox", "libs/go/coordination/outbox/postgres",
		"libs/go/storage/sql",
	},
	RoleWebhookDispatcher: {
		"libs/go/audit", "libs/go/audit/postgres", "libs/go/idempotency", "libs/go/idempotency/postgres",
		"libs/go/messaging", "libs/go/messaging/pubsub", "libs/go/signing", "libs/go/httpx/outbound",
		"libs/go/coordination/outbox", "libs/go/coordination/outbox/postgres",
		"libs/go/coordination/workqueue", "libs/go/coordination/workqueue/postgres",
		"libs/go/storage/sql",
	},
	RoleMaintenance: {
		"libs/go/audit", "libs/go/audit/postgres", "libs/go/storage/lease", "libs/go/storage/lease/postgres",
		"libs/go/coordination/leadership", "libs/go/coordination/workqueue", "libs/go/coordination/workqueue/postgres",
		"libs/go/storage/sql", "libs/go/storage/sql/migrate",
	},
}

// ConsumptionFor returns an isolated, sorted package inventory for role.
func ConsumptionFor(role Role) (Consumption, error) {
	values, ok := roleConsumption[role]
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
	set := make(map[string]struct{}, len(commonConsumption)+len(values))
	for _, packagePath := range append(append([]string(nil), commonConsumption...), values...) {
		set[packagePath] = struct{}{}
	}
	packages := make([]string, 0, len(set))
	for packagePath := range set {
		packages = append(packages, packagePath)
	}
	sort.Strings(packages)
	return Consumption{Role: role, Packages: packages}, nil
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
