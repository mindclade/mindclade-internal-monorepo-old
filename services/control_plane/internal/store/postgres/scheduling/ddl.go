// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package schedulingpostgres

import (
	"fmt"
	"strings"

	"go.mindclade.dev/control/scheduling"
	"go.mindclade.dev/libs/go/faults"
	sqlpostgres "go.mindclade.dev/libs/go/storage/sql/postgres"
)

// maximumPostgresIdentifierBytes is PostgreSQL's NAMEDATALEN-1 limit. Index
// names are derived from their table, so an index for a long table name must be
// truncated here rather than silently truncated by the server, where two
// distinct names could collide into one.
const maximumPostgresIdentifierBytes = 63

// unitSeparatorOrdinal is the byte scheduling.PlacementKey joins on. The
// placement key is reconstructed inside a CHECK constraint below, so the
// separator has to be nameable in SQL; chr(31) is that name.
//
// Each #>> extraction in that constraint is parenthesized, and has to be:
// PostgreSQL gives || a higher precedence than the generic jsonb operators, so
// an unparenthesized `document#>>'{a}' || chr(31)` parses as
// `document #>> ('{a}' || chr(31))` and fails to resolve at CREATE TABLE time.
const unitSeparatorOrdinal = 31

// demandColumns is the projection of one Demand vector onto five bigint
// columns, in the order control/scheduling declares its ResourceGroup.
//
// One column per covered resource rather than a jsonb operator in the WHERE
// clause, because the capacity ledger is rebuilt by summing this projection on
// every Snapshot, every Held, and every Reserve. SUM over a jsonb extraction
// cannot use an index; SUM over these columns is served by the covering partial
// index below without touching the heap. The pairing of column suffix to
// resource name lives here once so the DDL, the write path, and the ledger
// aggregate cannot disagree about which column holds which resource.
var demandColumns = [...]struct {
	suffix   string
	resource scheduling.ResourceName
}{
	{"cpu", scheduling.ResourceCPU},
	{"memory", scheduling.ResourceMemory},
	{"ephemeral_storage", scheduling.ResourceEphemeralStorage},
	{"gpu", scheduling.ResourceGPU},
	{"pods", scheduling.ResourcePods},
}

// DDL returns the scheduling schemas in apply order.
//
// Each row keeps the whole domain record in a jsonb `document` column and
// projects out only what is queried, constrained, or compared. The projection
// is derived on write and never read back into a domain value: every read below
// reconstructs the record from `document` and revalidates it, so a projected
// column that drifted can corrupt a query plan's answer but never a returned
// record. The CHECK constraints are what stop that drift from happening at all.
//
// The document keys are the JSON tags control/scheduling declares, not Go field
// names -- unlike the orchestration schema, these types are tagged. That is
// still a coupling worth naming: renaming a tag changes the on-disk key, and
// the constraints below are what turn that into a loud write failure instead of
// a silent one.
//
// Two encoding facts drive the column nullability, and both are easy to get
// backwards. time.Time is a struct, so `omitempty` does not omit it: BoundAt
// and FinalizedAt always serialize, as the zero instant, and their columns are
// therefore NOT NULL rather than nullable. identifiers.ID, identifiers.Digest,
// and resourceversion.Version marshal their zero values to JSON null, so
// preemptor_id is nullable and compared with IS NOT DISTINCT FROM. The
// NULLIF-against-the-empty-string form the orchestration schema uses for its
// untagged optional strings would never match here, because the value on disk
// is JSON null rather than an empty string.
func DDL(reservationTable, quotaTable, weightTable, ledgerTable string) ([]string, error) {
	const operation = "scheduling.postgres.DDL"
	for _, table := range []string{reservationTable, quotaTable, weightTable, ledgerTable} {
		if err := checkTable(table, operation); err != nil {
			return nil, err
		}
	}

	// The reservation is keyed by its own ID, and the placement key carries a
	// separate UNIQUE constraint rather than being the primary key. Both
	// identities are real and neither derives the other: the domain addresses a
	// reservation by ID, while PlacementKey is the idempotency identity that
	// makes a retried placement replay instead of double-charging the ledger.
	// Collapsing them would make a replayed placement unaddressable by the ID
	// the first attempt returned.
	reservations := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
    reservation_id      text PRIMARY KEY,
    placement_key       text NOT NULL UNIQUE,
    capacity_domain     text NOT NULL CHECK (capacity_domain IN (%s)),
    tenant              text NOT NULL,
    run_id              text NOT NULL,
    stage_id            text NOT NULL,
    attempt             bigint NOT NULL CHECK (attempt > 0),
    state               text NOT NULL CHECK (state IN (%s)),
    lease_fence         bigint NOT NULL CHECK (lease_fence > 0),
    sequence            bigint NOT NULL CHECK (sequence >= 0),
    created_at          timestamptz NOT NULL,
    expires_at          timestamptz NOT NULL CHECK (expires_at > created_at),
    bound_at            timestamptz NOT NULL,
    finalized_at        timestamptz NOT NULL,
    preemptor_id        text NULL,
    resource_version    text NOT NULL,
    resource_generation bigint NOT NULL CHECK (resource_generation > 0),
%s
    document            jsonb NOT NULL,
    written_at          timestamptz NOT NULL,
    CHECK (resource_generation = sequence + 1),
    CHECK (capacity_domain <> %s OR total_gpu = 0),
    CHECK (jsonb_typeof(document) = 'object'),
    CHECK (document->>'id' IS NOT DISTINCT FROM reservation_id),
    CHECK (placement_key IS NOT DISTINCT FROM
        (document#>>'{placement,run_id}') || chr(%d) || (document#>>'{placement,stage_id}') || chr(%d) || (document#>>'{placement,attempt}')),
    CHECK (document#>>'{placement,pool,domain}' IS NOT DISTINCT FROM capacity_domain),
    CHECK (document#>>'{placement,tenant}' IS NOT DISTINCT FROM tenant),
    CHECK (document#>>'{placement,run_id}' IS NOT DISTINCT FROM run_id),
    CHECK (document#>>'{placement,stage_id}' IS NOT DISTINCT FROM stage_id),
    CHECK ((document#>>'{placement,attempt}')::numeric IS NOT DISTINCT FROM attempt),
    CHECK (document->>'state' IS NOT DISTINCT FROM state),
    CHECK ((document->>'lease_fence')::numeric IS NOT DISTINCT FROM lease_fence),
    CHECK ((document->>'sequence')::numeric IS NOT DISTINCT FROM sequence),
    CHECK ((document->>'created_at')::timestamptz IS NOT DISTINCT FROM created_at),
    CHECK ((document->>'expires_at')::timestamptz IS NOT DISTINCT FROM expires_at),
    CHECK ((document->>'bound_at')::timestamptz IS NOT DISTINCT FROM bound_at),
    CHECK ((document->>'finalized_at')::timestamptz IS NOT DISTINCT FROM finalized_at),
    CHECK (document->>'preemptor' IS NOT DISTINCT FROM preemptor_id),
    CHECK (document->>'resource_version' IS NOT DISTINCT FROM resource_version),
    CHECK (split_part(resource_version, ':', 2)::numeric IS NOT DISTINCT FROM resource_generation)%s
);

CREATE INDEX IF NOT EXISTS %s
    ON %s (capacity_domain, tenant)
    INCLUDE (%s)
    WHERE state IN (%s);

CREATE INDEX IF NOT EXISTS %s
    ON %s (expires_at)
    WHERE state = 'held';
`,
		reservationTable,
		domainLiterals(), reservationStateLiterals(),
		demandColumnDefinitions("total_"),
		batchDomainLiteral(),
		unitSeparatorOrdinal, unitSeparatorOrdinal,
		demandDocumentChecks("total_", "placement", "total"),
		indexName(reservationTable, "ledger_idx"), reservationTable,
		demandColumnNames("total_"), occupyingStateLiterals(),
		indexName(reservationTable, "expiry_idx"), reservationTable)

	// One row per capacity domain, keyed by the domain itself. The domain's
	// text form is its workload class, which is the whole identity -- the
	// namespace and queue-name components are derived, and storing them would
	// invite a reader to trust a namespace that disagreed with the class.
	//
	// A zero nominal quota is a valid recorded state and means the queue is
	// held. It is not the same as an absent row, which means nobody measured
	// the domain at all, so there is no DEFAULT here and no seeding.
	quotas := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
    capacity_domain text PRIMARY KEY CHECK (capacity_domain IN (%s)),
%s
    document        jsonb NOT NULL,
    written_at      timestamptz NOT NULL,
    CHECK (capacity_domain <> %s OR nominal_gpu = 0),
    CHECK (jsonb_typeof(document) = 'object'),
    CHECK (document->>'domain' IS NOT DISTINCT FROM capacity_domain)%s
);
`,
		quotaTable, domainLiterals(),
		demandColumnDefinitions("nominal_"), batchDomainLiteral(),
		demandDocumentChecks("nominal_", "nominal"))

	// A weight is a scalar, so this table carries a document only to keep the
	// projection rule uniform: every column in this schema is checkable against
	// the record it was derived from, and a table with nothing to check against
	// would be the one place a silent projection drift could live.
	weights := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
    tenant     text PRIMARY KEY,
    weight     bigint NOT NULL CHECK (weight > 0 AND weight <= %d),
    document   jsonb NOT NULL,
    written_at timestamptz NOT NULL,
    CHECK (jsonb_typeof(document) = 'object'),
    CHECK (document->>'tenant' IS NOT DISTINCT FROM tenant),
    CHECK ((document->>'weight')::numeric IS NOT DISTINCT FROM weight)
);
`, weightTable, uint64(scheduling.MaximumShareWeight))

	// The singleton ledger row. It is not a projection of a domain record --
	// there is no domain type for it -- so it carries no document; its
	// constraints are range checks instead.
	//
	// The boolean primary key with CHECK (singleton) is what makes "exactly one
	// row" a schema property rather than a convention. A second row cannot be
	// inserted, so no code path has to defend against one, and every mutation
	// can lock this row by predicate without a parameter.
	//
	// The seed is part of the DDL rather than of the first write. Minting the
	// row lazily would put an INSERT on the hottest path in the package, and an
	// epoch of zero is not a valid FleetSnapshot epoch -- the row has to exist,
	// with epoch 1, before anything reads it.
	ledger := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
    singleton  boolean PRIMARY KEY CHECK (singleton),
    fence      bigint NOT NULL CHECK (fence >= 0),
    epoch      bigint NOT NULL CHECK (epoch > 0),
    updated_at timestamptz NOT NULL
);

INSERT INTO %s (singleton, fence, epoch, updated_at)
VALUES (true, 0, 1, now())
ON CONFLICT (singleton) DO NOTHING;
`, ledgerTable, ledgerTable)

	return []string{reservations, quotas, weights, ledger}, nil
}

// demandColumnDefinitions renders the five projected amount columns. The upper
// bound is the domain's own MaximumResourceAmount, so a row that would make a
// later SUM meaningless is refused by the server rather than by a Go check that
// an out-of-band writer could bypass.
func demandColumnDefinitions(prefix string) string {
	var builder strings.Builder
	for _, column := range demandColumns {
		fmt.Fprintf(&builder, "    %-19s bigint NOT NULL CHECK (%s >= 0 AND %s <= %d),\n",
			prefix+column.suffix, prefix+column.suffix, prefix+column.suffix,
			scheduling.MaximumResourceAmount)
	}
	return builder.String()
}

func demandColumnNames(prefix string) string {
	names := make([]string, 0, len(demandColumns))
	for _, column := range demandColumns {
		names = append(names, prefix+column.suffix)
	}
	return strings.Join(names, ", ")
}

// demandDocumentChecks ties each projected amount back to the document.
//
// COALESCE, not a bare cast: an absent key in a Demand map means zero, and
// jsonb's #>> returns SQL NULL for an absent key. Comparing NULL to a NOT NULL
// zero column would fail every write whose demand omitted a resource, which is
// the normal shape of a batch-cpu demand that names no GPU.
//
// The path is given in array form because two of the five resource names --
// ephemeral-storage and nvidia.com/gpu -- contain a hyphen and a dot. The
// text-path form document#>>'{a,b}' treats those as ordinary characters, while
// the dotted operator form would read nvidia.com/gpu as a nested path.
func demandDocumentChecks(prefix string, path ...string) string {
	var builder strings.Builder
	for _, column := range demandColumns {
		elements := append(append([]string(nil), path...), string(column.resource))
		fmt.Fprintf(&builder, ",\n    CHECK (COALESCE((document#>>'{%s}')::numeric, 0) IS NOT DISTINCT FROM %s)",
			strings.Join(elements, ","), prefix+column.suffix)
	}
	return builder.String()
}

// domainLiterals renders the three admissible capacity domains. It is derived
// from scheduling.Domains() rather than typed out, so a fourth workload class
// would reach the schema instead of being silently rejected at write time by a
// CHECK nobody remembered to widen.
func domainLiterals() string {
	domains := scheduling.Domains()
	values := make([]string, 0, len(domains))
	for _, domain := range domains {
		values = append(values, quoteLiteral(string(domain.WorkloadClass())))
	}
	return strings.Join(values, ", ")
}

// batchDomainLiteral names the one domain whose ClusterQueue omits
// nvidia.com/gpu from its coveredResources. A positive GPU amount there is not
// a shortage more nodes would fix; the queue has no dimension to charge it
// against, so the schema refuses to record one.
func batchDomainLiteral() string {
	return quoteLiteral(string(scheduling.WorkloadClassBatchCPU))
}

// reservationStateLiterals is the reservation lifecycle. The domain declares no
// exported list of its states, so this restates them; a state the domain adds
// and this omits fails its first write loudly rather than being stored as a
// value no query filters on.
func reservationStateLiterals() string {
	return stateLiterals(
		scheduling.ReservationHeld, scheduling.ReservationBound,
		scheduling.ReservationCompleted, scheduling.ReservationReleased,
		scheduling.ReservationExpired, scheduling.ReservationPreempted)
}

// occupyingStateLiterals is the non-terminal set: exactly the states for which
// Reservation.HeldDemand returns a charge. The covering index is partial on
// this predicate because the ledger only ever sums these rows, and a terminal
// reservation is history rather than capacity.
func occupyingStateLiterals() string {
	return stateLiterals(scheduling.ReservationHeld, scheduling.ReservationBound)
}

func stateLiterals(states ...scheduling.ReservationState) string {
	values := make([]string, 0, len(states))
	for _, state := range states {
		values = append(values, quoteLiteral(string(state)))
	}
	return strings.Join(values, ", ")
}

// quoteLiteral renders a SQL string literal. Every caller passes a compiled-in
// domain constant, so this cannot see attacker input; it doubles embedded
// quotes anyway, because a constant that later gains an apostrophe should
// produce a wrong-looking schema rather than a syntactically broken one.
func quoteLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func checkTable(table, operation string) error {
	if !sqlpostgres.ValidQualifiedIdentifier(strings.TrimSpace(table)) {
		return faults.New(faults.CodeInvalidArgument, "scheduling table name is invalid",
			faults.WithReason("scheduling_invalid_table"), faults.WithOperation(operation),
			faults.WithRetryPolicy(faults.NoRetry()))
	}
	return nil
}

func indexName(table, suffix string) string {
	base := strings.ReplaceAll(table, ".", "_")
	maximumBase := maximumPostgresIdentifierBytes - len(suffix) - 1
	if len(base) > maximumBase {
		base = base[:maximumBase]
	}
	return base + "_" + suffix
}
