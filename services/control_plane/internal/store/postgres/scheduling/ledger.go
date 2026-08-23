// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package schedulingpostgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"go.mindclade.dev/control/scheduling"
	"go.mindclade.dev/libs/go/faults"
)

const (
	// MaximumExpirySweep bounds how many expired holds one mutation re-seals.
	//
	// The sweep has to be bounded -- it runs inside every mutation's
	// transaction, and an unbounded one would let a backlog turn a Snapshot
	// into a minutes-long write. Refusing the mutation when the backlog is
	// larger would be worse: it would wedge the store, since every path that
	// could drain the backlog is itself a mutation. So the sweep takes the
	// oldest batch and returns.
	//
	// The number is small because of what the batch costs and where it is held.
	// Each expiry is a reservation UPDATE plus an audit row plus an outbox row,
	// and the whole batch runs while this transaction holds the singleton
	// ledger row that every other mutation must acquire first. Measured on a
	// loopback server with nothing else running, 256 expiries in one Snapshot
	// took 103ms; 64 is roughly a quarter of that. The failure that sizing
	// avoids is a control plane returning from a longer-than-TTL outage with
	// thousands of lapsed holds: mutations queue on the ledger row, and with a
	// 512-sized batch a concurrent Reserve could burn schedulingRetryMaxElapsed
	// waiting out a handful of sweeps and surface an aborted fault during
	// recovery, which is precisely when the sweep is supposed to be helping.
	//
	// A small batch does not slow the drain. Draining is proportional to how
	// often a mutation runs, and releasing the ledger row four times as often
	// lets four times as many mutations run -- each of which sweeps. What the
	// small batch buys is tail latency on the lock, not throughput.
	//
	// Being behind is safe in the only direction that matters. An expired hold
	// this sweep did not reach is still counted as occupied, so the ledger
	// reports less free capacity than there is: it refuses an admission rather
	// than over-committing one.
	MaximumExpirySweep = 64

	// MaximumLedgerGroups bounds the fair-share aggregate. It is the domain's
	// own ceiling -- three capacity domains times the fair-share claim bound --
	// so a result larger than this describes a fleet control/scheduling could
	// not evaluate anyway. This is an adapter bound with no memory-adapter
	// counterpart, so its reason string is local to this package.
	MaximumLedgerGroups = scheduling.MaximumSnapshotDomains * scheduling.MaximumShareClaims
)

// ledgerState is the singleton row: the highest leadership fence this store has
// accepted a write from, and the epoch every snapshot is stamped with.
type ledgerState struct {
	fence uint64
	epoch uint64
}

// lockLedger takes the store-wide write lock and reads the fence and epoch.
//
// Every mutation calls this first, before it touches any other row. That is the
// whole of divergence four. Orchestration could lock the row it was about to
// write, because its preconditions were row-local; here the fence is fleet-wide
// authority and the epoch is a single counter, so there is one row and every
// writer wants it. Taking it first and always is what turns "two writers that
// touch overlapping rows in different orders" -- a deadlock -- into a queue.
//
// It also makes SQLSTATE 40001 the ordinary outcome of contention rather than a
// rare one: under SERIALIZABLE a blocked locking read cannot fall forward onto
// the version the winner just wrote, so the loser is aborted here, at the first
// statement, before it has done any work worth losing. config.go sizes the
// retry budget from exactly that.
func (store *Store) lockLedger(ctx context.Context, operation string) (ledgerState, error) {
	query := fmt.Sprintf(`SELECT fence, epoch FROM %s WHERE singleton FOR UPDATE`, store.ledger)
	var fence, epoch int64
	err := store.executor(ctx).QueryRowContext(ctx, query).Scan(&fence, &epoch)
	if errors.Is(err, sql.ErrNoRows) {
		// DDL seeds this row, so a missing one means the schema was created by
		// something other than DDL. Seeding it here rather than failing keeps a
		// hand-built schema usable; ON CONFLICT DO NOTHING keeps the recovery
		// idempotent if two writers reach it at once.
		seed := fmt.Sprintf(`INSERT INTO %s (singleton, fence, epoch, updated_at)
VALUES (true, 0, 1, $1) ON CONFLICT (singleton) DO NOTHING`, store.ledger)
		if _, execErr := store.executor(ctx).ExecContext(ctx, seed, store.clock.Now().Round(0).UTC()); execErr != nil {
			return ledgerState{}, provider(ctx, execErr, operation+".Ledger")
		}
		err = store.executor(ctx).QueryRowContext(ctx, query).Scan(&fence, &epoch)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ledgerState{}, internal(ctx, err, operation, "scheduling_ledger_missing")
	}
	if err != nil {
		return ledgerState{}, provider(ctx, err, operation+".Ledger")
	}
	if fence < 0 || epoch <= 0 {
		return ledgerState{}, internal(ctx, nil, operation, "scheduling_ledger_corrupt")
	}
	return ledgerState{fence: uint64(fence), epoch: uint64(epoch)}, nil
}

// advanceLedger records the accepted fence and mints the next epoch.
//
// The memory adapter bumps its epoch on exactly the mutations that change what
// a snapshot would say -- a recorded quota, a recorded weight, an applied
// reservation or transition or preemption -- and on none of the replays. This
// mirrors that, which is why the expiry sweep does not call it: expiry changes
// reservations without changing the decision anyone made, and the reference
// adapter's expireLocked leaves the epoch alone.
func (store *Store) advanceLedger(ctx context.Context, fence uint64, at time.Time, operation string) error {
	accepted, err := sqlUint(ctx, fence, "lease_fence", operation)
	if err != nil {
		return err
	}
	query := fmt.Sprintf(`UPDATE %s SET fence=$1, epoch=epoch+1, updated_at=$2 WHERE singleton`, store.ledger)
	if _, execErr := store.executor(ctx).ExecContext(ctx, query, accepted, at.Round(0).UTC()); execErr != nil {
		return provider(ctx, execErr, operation+".Ledger")
	}
	return nil
}

// expire re-seals every hold whose deadline has passed, bounded by
// MaximumExpirySweep.
//
// This is why Snapshot and Held are mutations. The domain's rule is that an
// expired hold never appears as occupied capacity, because "that is the
// difference between capacity that is busy and capacity that is merely
// unaccounted for" -- so the sweep has to happen before any ledger read, inside
// the same transaction, or a reader could see capacity a deadline already
// released.
//
// The transition is scheduling.Reservation.Expire, not a state assignment.
// Expiry is authored by the deadline rather than by a leader, so it reuses the
// reservation's own fence: requiring a live one would make an unattended
// control plane unable to reclaim anything.
func (store *Store) expire(ctx context.Context, now time.Time, operation string) error {
	query := fmt.Sprintf(`SELECT document FROM %s
WHERE state=$1 AND expires_at <= $2 ORDER BY expires_at LIMIT %d FOR UPDATE`,
		store.reservations, MaximumExpirySweep)
	rows, err := store.executor(ctx).QueryContext(ctx, query, string(scheduling.ReservationHeld), now)
	if err != nil {
		return provider(ctx, err, operation+".Expire")
	}
	expired, err := scanReservations(ctx, rows, operation)
	if err != nil {
		return err
	}
	for _, reservation := range expired {
		sealed, expireErr := reservation.Expire(now, reservation.LeaseFence)
		if expireErr != nil {
			return expireErr
		}
		if writeErr := store.updateReservation(ctx, sealed, now, operation); writeErr != nil {
			return writeErr
		}
		// An expiry is a durable state change and a capacity release, so it is
		// published like any other transition. A silently expired reservation
		// is exactly the unaccounted-for capacity the sweep exists to prevent,
		// and a consumer that never hears about it keeps a phantom hold.
		//
		// deadlineAuthored, not the caller. The mutation that happens to run
		// the sweep did not cause these expiries, and recording an operator who
		// called Snapshot as the author of every hold that lapsed while they
		// were reading would make the audit log say something false.
		if emitErr := store.emitReservation(ctx, deadlineAuthored, sealed); emitErr != nil {
			return emitErr
		}
	}
	return nil
}

// quotaRecord is the stored form of one capacity domain's nominal quota. The
// domain has no aggregate for this -- MemoryRepository keeps a bare map -- so
// the record is defined here, with the domain inside the document so the
// projected key column has something to be checked against.
type quotaRecord struct {
	Domain  scheduling.CapacityDomain `json:"domain"`
	Nominal scheduling.Demand         `json:"nominal"`
}

func (record quotaRecord) Validate() error {
	return record.Nominal.ValidateForDomain(record.Domain, false)
}

// weightRecord is the stored form of one tenant's fair-share weight.
type weightRecord struct {
	Tenant string `json:"tenant"`
	Weight uint32 `json:"weight"`
}

func (record weightRecord) Validate() error { return validateWeight(record.Tenant, record.Weight) }

// validateWeight delegates the tenant-name and weight-range rules to the domain
// instead of restating them.
//
// control/scheduling exports no name validator -- validateName is unexported --
// but ShareClaim.Validate is exactly the pair of checks MemoryRepository.PutWeight
// makes, in the same order, and it raises the reference adapter's own faults:
// tenant_invalid, then share_weight_out_of_range. Reproducing the 128-character
// tenant alphabet here would be a second definition that could drift from the
// Kubernetes label shape it exists to mirror; borrowing the domain's cannot.
// The claim is validated against the batch-cpu domain with an empty usage
// vector, which every domain accepts, so only the two checks that matter fire.
func validateWeight(tenant string, weight uint32) error {
	claim := scheduling.ShareClaim{Tenant: tenant, Weight: weight, Used: make(scheduling.Demand)}
	return claim.Validate(scheduling.Domains()[0])
}

// PutQuota records the nominal quota one ClusterQueue grants. Zero quota is a
// valid recorded state and means the queue is held; it is not the same as an
// unrecorded domain, which means nobody measured it.
//
// It takes no transaction time, so unlike every other mutation here it does not
// sweep expiries first -- there is no `now` to sweep against. The reference
// adapter has the same shape and the same consequence: the held total this
// compares against may include a hold whose deadline has passed, which makes
// the reduction check strictly more conservative, never less.
func (store *Store) PutQuota(ctx context.Context, domain scheduling.CapacityDomain, nominal scheduling.Demand) error {
	const operation = "scheduling.postgres.PutQuota"
	if err := store.validate(ctx, operation); err != nil {
		return err
	}
	if err := nominal.ValidateForDomain(domain, false); err != nil {
		return err
	}
	record := quotaRecord{Domain: domain, Nominal: nominal.Clone()}
	document, err := marshalDocument(ctx, record, operation)
	if err != nil {
		return err
	}
	amounts, err := demandAmounts(ctx, nominal, operation)
	if err != nil {
		return err
	}
	class := string(domain.WorkloadClass())
	written := store.clock.Now().Round(0).UTC()
	_, err = runMutation(ctx, store, operation, func(txContext context.Context) (struct{}, error) {
		state, lockErr := store.lockLedger(txContext, operation)
		if lockErr != nil {
			return struct{}{}, lockErr
		}
		present, countErr := store.quotaDomainCount(txContext, class, operation)
		if countErr != nil {
			return struct{}{}, countErr
		}
		if present >= scheduling.MaximumSnapshotDomains {
			return struct{}{}, domainError(txContext, faults.CodeResourceExhausted,
				"quota_domain_bound", "capacity domain ledger bound was reached", operation)
		}
		// A quota may not be reduced below what is already held. Doing so would
		// leave the ledger permanently over-reserved with no transition able to
		// repair it, and every subsequent Validate would fail.
		held, heldErr := store.heldDemand(txContext, domain, operation)
		if heldErr != nil {
			return struct{}{}, heldErr
		}
		if !held.Fits(nominal) {
			return struct{}{}, domainError(txContext, faults.CodeFailedPrecondition,
				"quota_below_reserved", "nominal quota cannot be reduced below held capacity", operation)
		}
		query := fmt.Sprintf(`INSERT INTO %s (capacity_domain,%s,document,written_at)
VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,$8)
ON CONFLICT (capacity_domain) DO UPDATE SET
%s,document=EXCLUDED.document,written_at=EXCLUDED.written_at`,
			store.quotas, demandColumnNames("nominal_"), demandUpdateAssignments("nominal_"))
		arguments := append([]any{class}, amounts...)
		arguments = append(arguments, document, written)
		if _, execErr := store.executor(txContext).ExecContext(txContext, query, arguments...); execErr != nil {
			return struct{}{}, provider(txContext, execErr, operation)
		}
		if emitErr := store.emitQuota(txContext, domain, nominal, state.epoch+1); emitErr != nil {
			return struct{}{}, emitErr
		}
		// The fence is carried forward unchanged: recording a quota is
		// configuration, not a fenced capacity write, and the reference adapter
		// does not move its fence here either.
		return struct{}{}, store.advanceLedger(txContext, state.fence, written, operation)
	})
	return err
}

// PutWeight records a tenant's fair-share weight.
func (store *Store) PutWeight(ctx context.Context, tenant string, weight uint32) error {
	const operation = "scheduling.postgres.PutWeight"
	if err := store.validate(ctx, operation); err != nil {
		return err
	}
	if err := validateWeight(tenant, weight); err != nil {
		return err
	}
	document, err := marshalDocument(ctx, weightRecord{Tenant: tenant, Weight: weight}, operation)
	if err != nil {
		return err
	}
	written := store.clock.Now().Round(0).UTC()
	_, err = runMutation(ctx, store, operation, func(txContext context.Context) (struct{}, error) {
		state, lockErr := store.lockLedger(txContext, operation)
		if lockErr != nil {
			return struct{}{}, lockErr
		}
		present, countErr := store.weightTenantCount(txContext, tenant, operation)
		if countErr != nil {
			return struct{}{}, countErr
		}
		if present >= scheduling.MaximumShareClaims {
			return struct{}{}, domainError(txContext, faults.CodeResourceExhausted,
				"share_claim_bound", "fair-share claim bound was reached", operation)
		}
		query := fmt.Sprintf(`INSERT INTO %s (tenant,weight,document,written_at)
VALUES ($1,$2,$3::jsonb,$4)
ON CONFLICT (tenant) DO UPDATE SET
weight=EXCLUDED.weight,document=EXCLUDED.document,written_at=EXCLUDED.written_at`, store.weights)
		if _, execErr := store.executor(txContext).ExecContext(txContext, query,
			tenant, int64(weight), document, written); execErr != nil {
			return struct{}{}, provider(txContext, execErr, operation)
		}
		if emitErr := store.emitWeight(txContext, tenant, weight, state.epoch+1); emitErr != nil {
			return struct{}{}, emitErr
		}
		return struct{}{}, store.advanceLedger(txContext, state.fence, written, operation)
	})
	return err
}

// quotaDomainCount returns how many domains are recorded excluding the one
// about to be written, so the caller can compare against the domain bound
// without treating an overwrite as a new entry.
func (store *Store) quotaDomainCount(ctx context.Context, class, operation string) (int, error) {
	var total int
	err := store.executor(ctx).QueryRowContext(ctx,
		fmt.Sprintf(`SELECT count(*) FROM %s WHERE capacity_domain <> $1`, store.quotas), class).Scan(&total)
	if err != nil {
		return 0, provider(ctx, err, operation)
	}
	return total, nil
}

func (store *Store) weightTenantCount(ctx context.Context, tenant, operation string) (int, error) {
	var total int
	err := store.executor(ctx).QueryRowContext(ctx,
		fmt.Sprintf(`SELECT count(*) FROM %s WHERE tenant <> $1`, store.weights), tenant).Scan(&total)
	if err != nil {
		return 0, provider(ctx, err, operation)
	}
	return total, nil
}

// heldDemand sums what one domain currently charges to the ledger.
//
// The predicate is the non-terminal set, which is exactly the set for which
// Reservation.HeldDemand returns a charge. That equivalence is what lets the
// ledger be rebuilt from the reservation table alone rather than kept as a
// running total -- there is no second number here that could drift from the
// reservations it is supposed to summarize.
func (store *Store) heldDemand(ctx context.Context, domain scheduling.CapacityDomain, operation string) (scheduling.Demand, error) {
	query := fmt.Sprintf(`SELECT %s FROM %s WHERE capacity_domain=$1 AND state IN (%s)`,
		demandSumProjection("total_"), store.reservations, occupyingStateLiterals())
	row := store.executor(ctx).QueryRowContext(ctx, query, string(domain.WorkloadClass()))
	demand, err := scanDemand(ctx, row.Scan, operation)
	if err != nil {
		return nil, err
	}
	return demand, nil
}

// reservedByDomain is heldDemand for every domain in one pass.
func (store *Store) reservedByDomain(ctx context.Context, operation string) (map[scheduling.CapacityDomain]scheduling.Demand, error) {
	query := fmt.Sprintf(`SELECT capacity_domain,%s FROM %s WHERE state IN (%s)
GROUP BY capacity_domain LIMIT %d`,
		demandSumProjection("total_"), store.reservations, occupyingStateLiterals(),
		scheduling.MaximumSnapshotDomains+1)
	rows, err := store.executor(ctx).QueryContext(ctx, query)
	if err != nil {
		return nil, provider(ctx, err, operation)
	}
	defer func() { _ = rows.Close() }()
	reserved := make(map[scheduling.CapacityDomain]scheduling.Demand, scheduling.MaximumSnapshotDomains)
	for rows.Next() {
		var class string
		demand, scanErr := scanDemand(ctx, func(destinations ...any) error {
			return rows.Scan(append([]any{&class}, destinations...)...)
		}, operation)
		if scanErr != nil {
			return nil, scanErr
		}
		domain, domainErr := scheduling.DomainFor(scheduling.WorkloadClass(class))
		if domainErr != nil {
			return nil, internal(ctx, domainErr, operation, "scheduling_ledger_domain_invalid")
		}
		reserved[domain] = demand
	}
	if err := rows.Err(); err != nil {
		return nil, provider(ctx, err, operation)
	}
	if len(reserved) > scheduling.MaximumSnapshotDomains {
		return nil, domainError(ctx, faults.CodeResourceExhausted, "ledger_domain_bound",
			"capacity ledger names more domains than the fleet can have", operation)
	}
	return reserved, nil
}

// usageByTenant is the per-tenant half of the fair-share view.
//
// The join against the weight table is not an optimization, it is the claim-set
// rule: a FairShare carries one ShareClaim per weighted tenant, so usage is
// only ever needed for tenants that have a weight. It is also what bounds the
// result -- the weight table is capped at MaximumShareClaims on write, so this
// can return at most three domains times that, whereas grouping by every tenant
// that holds a reservation would be bounded by nothing.
//
// A tenant with usage and no weight is deliberately absent here. It is still
// counted in Reserved by reservedByDomain, because it really is holding
// capacity; it simply has no fair-share claim to rank.
func (store *Store) usageByTenant(ctx context.Context, operation string) (map[scheduling.CapacityDomain]map[string]scheduling.Demand, error) {
	query := fmt.Sprintf(`SELECT reservation.capacity_domain,reservation.tenant,%s
FROM %s AS reservation
JOIN %s AS weight ON weight.tenant = reservation.tenant
WHERE reservation.state IN (%s)
GROUP BY reservation.capacity_domain, reservation.tenant
LIMIT %d`,
		demandSumProjection("reservation.total_"), store.reservations, store.weights,
		occupyingStateLiterals(), MaximumLedgerGroups+1)
	rows, err := store.executor(ctx).QueryContext(ctx, query)
	if err != nil {
		return nil, provider(ctx, err, operation)
	}
	defer func() { _ = rows.Close() }()
	usage := make(map[scheduling.CapacityDomain]map[string]scheduling.Demand, scheduling.MaximumSnapshotDomains)
	groups := 0
	for rows.Next() {
		var class, tenant string
		demand, scanErr := scanDemand(ctx, func(destinations ...any) error {
			return rows.Scan(append([]any{&class, &tenant}, destinations...)...)
		}, operation)
		if scanErr != nil {
			return nil, scanErr
		}
		domain, domainErr := scheduling.DomainFor(scheduling.WorkloadClass(class))
		if domainErr != nil {
			return nil, internal(ctx, domainErr, operation, "scheduling_ledger_domain_invalid")
		}
		byTenant, exists := usage[domain]
		if !exists {
			byTenant = make(map[string]scheduling.Demand, scheduling.MaximumShareClaims)
			usage[domain] = byTenant
		}
		byTenant[tenant] = demand
		groups++
	}
	if err := rows.Err(); err != nil {
		return nil, provider(ctx, err, operation)
	}
	// Refused rather than truncated. A short fair-share view would under-report
	// a tenant's usage, which is the one direction fairness must never fail in.
	if groups > MaximumLedgerGroups {
		return nil, domainError(ctx, faults.CodeResourceExhausted, "ledger_group_bound",
			"fair-share ledger exceeds its bound", operation)
	}
	return usage, nil
}

func (store *Store) recordedQuotas(ctx context.Context, operation string) ([]quotaRecord, error) {
	query := fmt.Sprintf(`SELECT document FROM %s LIMIT %d`, store.quotas, scheduling.MaximumSnapshotDomains+1)
	rows, err := store.executor(ctx).QueryContext(ctx, query)
	if err != nil {
		return nil, provider(ctx, err, operation)
	}
	defer func() { _ = rows.Close() }()
	records := make([]quotaRecord, 0, scheduling.MaximumSnapshotDomains)
	for rows.Next() {
		var document []byte
		if scanErr := rows.Scan(&document); scanErr != nil {
			return nil, provider(ctx, scanErr, operation)
		}
		record, decodeErr := decodeDocument[quotaRecord](ctx, document, operation)
		if decodeErr != nil {
			return nil, decodeErr
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, provider(ctx, err, operation)
	}
	if len(records) > scheduling.MaximumSnapshotDomains {
		return nil, domainError(ctx, faults.CodeResourceExhausted, "snapshot_domain_bound",
			"fleet snapshot exceeds the capacity domain bound", operation)
	}
	// Sorted in Go on the domain's canonical triple, not by the server. The
	// reference adapter orders by CapacityDomain.String(), and a database
	// collation is free to disagree with byte order on the hyphens these names
	// contain -- which would fork the snapshot fingerprint between adapters.
	sort.Slice(records, func(left, right int) bool {
		return records[left].Domain.String() < records[right].Domain.String()
	})
	return records, nil
}

func (store *Store) recordedWeights(ctx context.Context, operation string) ([]weightRecord, error) {
	query := fmt.Sprintf(`SELECT document FROM %s LIMIT %d`, store.weights, scheduling.MaximumShareClaims+1)
	rows, err := store.executor(ctx).QueryContext(ctx, query)
	if err != nil {
		return nil, provider(ctx, err, operation)
	}
	defer func() { _ = rows.Close() }()
	records := make([]weightRecord, 0, 16)
	for rows.Next() {
		var document []byte
		if scanErr := rows.Scan(&document); scanErr != nil {
			return nil, provider(ctx, scanErr, operation)
		}
		record, decodeErr := decodeDocument[weightRecord](ctx, document, operation)
		if decodeErr != nil {
			return nil, decodeErr
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, provider(ctx, err, operation)
	}
	if len(records) > scheduling.MaximumShareClaims {
		return nil, domainError(ctx, faults.CodeResourceExhausted, "share_claim_bound",
			"fair-share claim bound was reached", operation)
	}
	sort.Slice(records, func(left, right int) bool { return records[left].Tenant < records[right].Tenant })
	return records, nil
}

// fleetSnapshot rebuilds the value the reference adapter's snapshotLocked
// builds, from committed rows, inside the caller's transaction.
//
// This is the piece Reserve compares a caller's decision against, so it has to
// agree with the memory adapter byte for byte in the canonical form -- not
// approximately, because the comparison is a digest. Three rules carry that:
//
//   - Domains come from the quota table and are ordered by the domain's own
//     String(). An unrecorded domain is absent, not empty: a snapshot that
//     invented a zero ledger for an unmeasured queue would let Reserve admit
//     against capacity nobody observed.
//   - Reserved is the sum over every non-terminal reservation in the domain,
//     whatever tenant holds it, including tenants with no fair-share weight.
//   - Claims are one per weighted tenant, sorted by tenant, present even at
//     zero usage. A tenant that has recorded a weight and used nothing is a
//     claim with an empty Used vector; dropping it would change the fingerprint
//     and make every decision taken against the reference adapter stale here.
func (store *Store) fleetSnapshot(ctx context.Context, epoch uint64, now time.Time, operation string) (scheduling.FleetSnapshot, error) {
	quotas, err := store.recordedQuotas(ctx, operation)
	if err != nil {
		return scheduling.FleetSnapshot{}, err
	}
	weights, err := store.recordedWeights(ctx, operation)
	if err != nil {
		return scheduling.FleetSnapshot{}, err
	}
	reserved, err := store.reservedByDomain(ctx, operation)
	if err != nil {
		return scheduling.FleetSnapshot{}, err
	}
	usage, err := store.usageByTenant(ctx, operation)
	if err != nil {
		return scheduling.FleetSnapshot{}, err
	}

	allocatables := make([]scheduling.Allocatable, 0, len(quotas))
	shares := make([]scheduling.FairShare, 0, len(quotas))
	for _, quota := range quotas {
		held := reserved[quota.Domain]
		if held == nil {
			held = make(scheduling.Demand)
		}
		allocatable := scheduling.Allocatable{
			Domain: quota.Domain, Nominal: quota.Nominal.Clone(), Reserved: held,
		}
		if validateErr := allocatable.Validate(); validateErr != nil {
			return scheduling.FleetSnapshot{}, validateErr
		}
		allocatables = append(allocatables, allocatable)

		claims := make([]scheduling.ShareClaim, 0, len(weights))
		for _, weight := range weights {
			used := usage[quota.Domain][weight.Tenant].Clone()
			if used == nil {
				used = make(scheduling.Demand)
			}
			claims = append(claims, scheduling.ShareClaim{
				Tenant: weight.Tenant, Weight: weight.Weight, Used: used,
			})
		}
		share := scheduling.FairShare{
			Domain: quota.Domain, Capacity: quota.Nominal.Clone(), Claims: claims,
		}
		if validateErr := share.Validate(); validateErr != nil {
			return scheduling.FleetSnapshot{}, validateErr
		}
		shares = append(shares, share)
	}
	snapshot := scheduling.FleetSnapshot{
		Epoch:          epoch,
		ObservedAt:     now,
		Allocatables:   allocatables,
		Shares:         shares,
		TopologyDigest: scheduling.TopologyFingerprint(),
	}
	if err := snapshot.Validate(); err != nil {
		return scheduling.FleetSnapshot{}, err
	}
	return snapshot, nil
}

// demandAmounts projects a Demand onto the five bigint columns, in the schema's
// column order, refusing an amount PostgreSQL's signed bigint cannot hold.
func demandAmounts(ctx context.Context, demand scheduling.Demand, operation string) ([]any, error) {
	amounts := make([]any, 0, len(demandColumns))
	for _, column := range demandColumns {
		amount, err := sqlUint(ctx, demand[column.resource], column.suffix, operation)
		if err != nil {
			return nil, err
		}
		amounts = append(amounts, amount)
	}
	return amounts, nil
}

// demandSumProjection renders the five aggregates. The cast back to bigint is
// deliberate: SUM over bigint yields numeric, and a ledger whose total no
// longer fits a bigint is corrupt rather than merely large, so the server
// should refuse it here instead of handing back a value Go would silently
// truncate.
func demandSumProjection(prefix string) string {
	sums := make([]string, 0, len(demandColumns))
	for _, column := range demandColumns {
		sums = append(sums, fmt.Sprintf("COALESCE(SUM(%s),0)::bigint", prefix+column.suffix))
	}
	return strings.Join(sums, ",")
}

func demandUpdateAssignments(prefix string) string {
	assignments := make([]string, 0, len(demandColumns))
	for _, column := range demandColumns {
		name := prefix + column.suffix
		assignments = append(assignments, name+"=EXCLUDED."+name)
	}
	return strings.Join(assignments, ",")
}

// scanDemand reads the five projected amounts back into a Demand. Every key is
// written, including the zeros: an absent key and a zero amount mean the same
// thing to the domain, but writing all five keeps the reconstructed vector
// identical in shape to the one the reference adapter's arithmetic produces.
func scanDemand(ctx context.Context, scan func(...any) error, operation string) (scheduling.Demand, error) {
	amounts := make([]int64, len(demandColumns))
	destinations := make([]any, 0, len(demandColumns))
	for index := range amounts {
		destinations = append(destinations, &amounts[index])
	}
	if err := scan(destinations...); err != nil {
		return nil, provider(ctx, err, operation)
	}
	demand := make(scheduling.Demand, len(demandColumns))
	for index, column := range demandColumns {
		if amounts[index] < 0 {
			return nil, internal(ctx, nil, operation, "scheduling_ledger_negative_amount")
		}
		demand[column.resource] = uint64(amounts[index])
	}
	return demand, nil
}
