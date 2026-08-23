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
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/resourceversion"
)

// MaximumHeldFetch bounds one Held request.
//
// It is the domain's own ceiling on a held set: SelectVictims refuses a set
// larger than three capacity domains times the fair-share claim bound, so a
// result past this point could not be turned into a preemption plan anyway.
// The list is refused rather than truncated -- a silently short held set would
// read as a domain with fewer holds than it has, and preemption would pick
// victims from a fleet it cannot see all of.
const MaximumHeldFetch = MaximumLedgerGroups

// reservationColumns is the write projection, in the order the schema declares
// it. The five demand columns are spliced in from demandColumns so the SQL and
// the DDL cannot disagree about which column holds which resource.
func reservationColumns() string {
	return "reservation_id,placement_key,capacity_domain,tenant,run_id,stage_id,attempt," +
		"state,lease_fence,sequence,created_at,expires_at,bound_at,finalized_at,preemptor_id," +
		"resource_version,resource_generation," + demandColumnNames("total_") + ",document,written_at"
}

func placeholders(count int) string {
	values := make([]string, 0, count)
	for index := 1; index <= count; index++ {
		values = append(values, fmt.Sprintf("$%d", index))
	}
	return strings.Join(values, ",")
}

// Snapshot returns the capacity and fairness view one admission decision is
// evaluated against.
//
// It is a mutation. Divergence one: it re-seals every expired hold before it
// reads the ledger, because the domain's rule is that an expired hold never
// appears as occupied capacity. A snapshot that reported a lapsed hold as busy
// would refuse admissions the fleet has room for, and one taken outside a
// transaction could not make the sweep and the read agree at all.
func (store *Store) Snapshot(ctx context.Context, now time.Time) (scheduling.FleetSnapshot, error) {
	const operation = "scheduling.postgres.Snapshot"
	if err := store.validate(ctx, operation); err != nil {
		return scheduling.FleetSnapshot{}, err
	}
	if now.IsZero() {
		return scheduling.FleetSnapshot{}, domainError(ctx, faults.CodeInvalidArgument,
			"snapshot_time_invalid", "snapshot time is required", operation)
	}
	observed := now.Round(0).UTC()
	return runMutation(ctx, store, operation, func(txContext context.Context) (scheduling.FleetSnapshot, error) {
		state, err := store.lockLedger(txContext, operation)
		if err != nil {
			return scheduling.FleetSnapshot{}, err
		}
		if err := store.expire(txContext, observed, operation); err != nil {
			return scheduling.FleetSnapshot{}, err
		}
		return store.fleetSnapshot(txContext, state.epoch, observed, operation)
	})
}

// Held returns the reservations still occupying capacity in one domain.
//
// Also a mutation, for the same reason as Snapshot: preemption chooses victims
// from this list, and a lapsed hold offered as a victim would be work destroyed
// to reclaim capacity that was already free.
func (store *Store) Held(ctx context.Context, domain scheduling.CapacityDomain, now time.Time) ([]scheduling.Reservation, error) {
	const operation = "scheduling.postgres.Held"
	if err := store.validate(ctx, operation); err != nil {
		return nil, err
	}
	if err := domain.Validate(); err != nil {
		return nil, err
	}
	if now.IsZero() {
		return nil, domainError(ctx, faults.CodeInvalidArgument,
			"snapshot_time_invalid", "snapshot time is required", operation)
	}
	observed := now.Round(0).UTC()
	return runMutation(ctx, store, operation, func(txContext context.Context) ([]scheduling.Reservation, error) {
		if _, err := store.lockLedger(txContext, operation); err != nil {
			return nil, err
		}
		if err := store.expire(txContext, observed, operation); err != nil {
			return nil, err
		}
		// The predicate is Reservation.Active spelled in SQL: bound always
		// occupies, held occupies until its deadline. The deadline clause is
		// not redundant with the sweep above -- the sweep is bounded, and this
		// keeps the answer correct while a backlog drains.
		query := fmt.Sprintf(`SELECT document FROM %s
WHERE capacity_domain=$1 AND (state=$2 OR (state=$3 AND expires_at > $4)) LIMIT %d`,
			store.reservations, MaximumHeldFetch+1)
		rows, err := store.executor(txContext).QueryContext(txContext, query,
			string(domain.WorkloadClass()), string(scheduling.ReservationBound),
			string(scheduling.ReservationHeld), observed)
		if err != nil {
			return nil, provider(txContext, err, operation)
		}
		active, err := scanReservations(txContext, rows, operation)
		if err != nil {
			return nil, err
		}
		if len(active) > MaximumHeldFetch {
			return nil, domainError(txContext, faults.CodeResourceExhausted,
				"held_reservation_bound", "held reservation set exceeds its bound", operation)
		}
		// Ordered in Go on the identifier's own bytes. The reference adapter
		// sorts by ID.String(), and a server-side ORDER BY would order by the
		// database collation, which is free to disagree about the underscores
		// and digits a canonical identifier contains.
		sort.Slice(active, func(left, right int) bool {
			return active[left].ID.String() < active[right].ID.String()
		})
		return active, nil
	})
}

// Get returns one reservation. It is the only pure read in this package: it
// names a single row, consults no ledger, and therefore has nothing to expire.
// A held reservation past its deadline is returned as held, exactly as the
// reference adapter's Get returns it -- expiry is a ledger concern, and Get is
// not a ledger read.
func (store *Store) Get(ctx context.Context, id identifiers.ID) (scheduling.Reservation, error) {
	const operation = "scheduling.postgres.Get"
	if err := store.validate(ctx, operation); err != nil {
		return scheduling.Reservation{}, err
	}
	if err := validateReservationID(ctx, id, operation); err != nil {
		return scheduling.Reservation{}, err
	}
	var document []byte
	err := store.executor(ctx).QueryRowContext(ctx,
		fmt.Sprintf(`SELECT document FROM %s WHERE reservation_id=$1`, store.reservations),
		id.String()).Scan(&document)
	if errors.Is(err, sql.ErrNoRows) {
		return scheduling.Reservation{}, domainError(ctx, faults.CodeNotFound,
			"reservation_not_found", "reservation was not found", operation)
	}
	if err != nil {
		return scheduling.Reservation{}, provider(ctx, err, operation)
	}
	decoded, err := decodeDocument[reservationDocument](ctx, document, operation)
	if err != nil {
		return scheduling.Reservation{}, err
	}
	return scheduling.Reservation(decoded), nil
}

// Reserve records a capacity hold, re-checking the fleet inside the write.
//
// Divergence three, and the reason this method is the largest in the package.
// The caller decided against a snapshot it read earlier; this rebuilds that
// snapshot from committed rows and compares fingerprints. Comparing rather than
// re-running admission keeps the decision the caller's -- the store does not
// get to second-guess which tenant should win -- while making a decision taken
// against a fleet that has since moved impossible to commit. Two schedulers
// admitting against the same stale view is exactly how a queue gets
// over-committed, and it is the failure this comparison exists to stop.
//
// The store bound the reference adapter enforces here is deliberately absent.
// Refusing to record a reservation because the table has many rows would leave
// the cluster holding capacity this ledger cannot see, which is data loss
// wearing a capacity signal's clothes.
func (store *Store) Reserve(
	ctx context.Context,
	snapshot scheduling.FleetSnapshot,
	candidate scheduling.Reservation,
	now time.Time,
) (scheduling.Reservation, bool, error) {
	const operation = "scheduling.postgres.Reserve"
	if err := store.validate(ctx, operation); err != nil {
		return scheduling.Reservation{}, false, err
	}
	if err := snapshot.Validate(); err != nil {
		return scheduling.Reservation{}, false, err
	}
	if err := candidate.Validate(); err != nil {
		return scheduling.Reservation{}, false, err
	}
	if candidate.State != scheduling.ReservationHeld || candidate.Sequence != 0 {
		return scheduling.Reservation{}, false, domainError(ctx, faults.CodeInvalidArgument,
			"reservation_initial_state_invalid", "reservation is not in its initial state", operation)
	}
	if now.IsZero() {
		return scheduling.Reservation{}, false, domainError(ctx, faults.CodeInvalidArgument,
			"snapshot_time_invalid", "reservation time is required", operation)
	}
	transactionTime := now.Round(0).UTC()
	if !candidate.CreatedAt.Equal(transactionTime) {
		return scheduling.Reservation{}, false, domainError(ctx, faults.CodeInvalidArgument,
			"reservation_created_at_invalid",
			"reservation creation time does not match the transaction time", operation)
	}
	expected := snapshot.Fingerprint()
	key := scheduling.PlacementKey(candidate.Placement)
	domain := candidate.Placement.Pool.Domain

	result, err := runMutation(ctx, store, operation, func(txContext context.Context) (transitionResult, error) {
		state, lockErr := store.lockLedger(txContext, operation)
		if lockErr != nil {
			return transitionResult{}, lockErr
		}
		if expireErr := store.expire(txContext, transactionTime, operation); expireErr != nil {
			return transitionResult{}, expireErr
		}

		// The placement key is a UNIQUE column on the reservation row rather
		// than a separate index, so there is no "index references a missing
		// reservation" case to defend against here: finding the key is finding
		// the row.
		existing, found, lookupErr := store.lockReservation(txContext,
			`placement_key=$1`, key, operation)
		if lookupErr != nil {
			return transitionResult{}, lookupErr
		}
		if found {
			if !existing.SamePlacement(candidate) {
				return transitionResult{}, domainError(txContext, faults.CodeConflict,
					"placement_key_reused", "placement key was reused for a different placement", operation)
			}
			if existing.State.Terminal() {
				return transitionResult{}, domainError(txContext, faults.CodeFailedPrecondition,
					"reservation_terminal", "the reservation for this placement is already terminal", operation)
			}
			return transitionResult{reservation: existing, replayed: true}, nil
		}
		if _, exists, idErr := store.lockReservation(txContext,
			`reservation_id=$1`, candidate.ID.String(), operation); idErr != nil {
			return transitionResult{}, idErr
		} else if exists {
			return transitionResult{}, domainError(txContext, faults.CodeConflict,
				"reservation_id_conflict", "reservation ID already exists", operation)
		}
		if candidate.LeaseFence < state.fence {
			return transitionResult{}, domainError(txContext, faults.CodeConflict,
				"lease_fence_stale", "writer holds an older leadership fence than the store", operation)
		}

		// The snapshot the caller decided against must still describe the
		// store. The fingerprint covers the observation time as well as the
		// ledgers and claims, so the caller has to have taken its snapshot at
		// this same transaction time -- which is what Service.Place does, and
		// what makes the comparison a decision check rather than a clock race.
		current, snapshotErr := store.fleetSnapshot(txContext, state.epoch, transactionTime, operation)
		if snapshotErr != nil {
			return transitionResult{}, snapshotErr
		}
		if !current.Fingerprint().Equal(expected) {
			return transitionResult{}, domainError(txContext, faults.CodeConflict,
				"fleet_snapshot_stale", "fleet snapshot is stale", operation)
		}
		allocatable, allocatableErr := current.Allocatable(domain)
		if allocatableErr != nil {
			return transitionResult{}, allocatableErr
		}
		if _, reserveErr := allocatable.Reserve(candidate.Placement.Total); reserveErr != nil {
			return transitionResult{}, reserveErr
		}

		if insertErr := store.insertReservation(txContext, candidate, key, transactionTime, operation); insertErr != nil {
			return transitionResult{}, insertErr
		}
		if emitErr := store.emitReservation(txContext, "scheduling.reservation.reserve", candidate); emitErr != nil {
			return transitionResult{}, emitErr
		}
		if ledgerErr := store.advanceLedger(txContext, candidate.LeaseFence, transactionTime, operation); ledgerErr != nil {
			return transitionResult{}, ledgerErr
		}
		return transitionResult{reservation: candidate}, nil
	})
	if err != nil {
		return scheduling.Reservation{}, false, err
	}
	return result.reservation, result.replayed, nil
}

// Bind records that the cluster accepted the workload.
func (store *Store) Bind(
	ctx context.Context, id identifiers.ID, expected resourceversion.Version,
	assignment scheduling.TopologyAssignment, fence uint64, now time.Time,
) (scheduling.Reservation, bool, error) {
	return store.transition(ctx, "scheduling.postgres.Bind", id, expected, fence, now,
		scheduling.ReservationBound, assignment)
}

// Complete releases capacity after the bound workload finished.
func (store *Store) Complete(
	ctx context.Context, id identifiers.ID, expected resourceversion.Version,
	fence uint64, now time.Time,
) (scheduling.Reservation, bool, error) {
	return store.transition(ctx, "scheduling.postgres.Complete", id, expected, fence, now,
		scheduling.ReservationCompleted, scheduling.TopologyAssignment{})
}

// Release returns held capacity before the workload was ever bound.
func (store *Store) Release(
	ctx context.Context, id identifiers.ID, expected resourceversion.Version,
	fence uint64, now time.Time,
) (scheduling.Reservation, bool, error) {
	return store.transition(ctx, "scheduling.postgres.Release", id, expected, fence, now,
		scheduling.ReservationReleased, scheduling.TopologyAssignment{})
}

// Expire returns held capacity after the deadline. Calling it early is a failed
// precondition rather than a no-op, because an early expiry would free capacity
// the holder is still entitled to use.
func (store *Store) Expire(
	ctx context.Context, id identifiers.ID, expected resourceversion.Version,
	fence uint64, now time.Time,
) (scheduling.Reservation, bool, error) {
	return store.transition(ctx, "scheduling.postgres.Expire", id, expected, fence, now,
		scheduling.ReservationExpired, scheduling.TopologyAssignment{})
}

type transitionResult struct {
	reservation scheduling.Reservation
	replayed    bool
}

// transition applies one lifecycle change under a fence and a version.
//
// Divergence two lives here. Orchestration had to restate an unexported digest
// because its transition was a field assignment followed by a re-seal; every
// scheduling transition is an exported method on Reservation that validates the
// source state, checks the timing, advances the sequence, re-seals the version
// and revalidates the result. So this reads the row, calls the method, and
// stores what the method returned. There is no second definition of the seal in
// this package and therefore nothing that can drift from the domain's.
//
// The check order is the reference adapter's, exactly: fence staleness before
// existence, existence before replay, replay before terminality, terminality
// before the version precondition. That order is observable -- a caller that
// switches on the reason gets the same answer from either adapter for a request
// that is wrong in more than one way.
func (store *Store) transition(
	ctx context.Context, operation string, id identifiers.ID, expected resourceversion.Version,
	fence uint64, now time.Time, target scheduling.ReservationState,
	assignment scheduling.TopologyAssignment,
) (scheduling.Reservation, bool, error) {
	if err := store.validate(ctx, operation); err != nil {
		return scheduling.Reservation{}, false, err
	}
	if err := validateReservationID(ctx, id, operation); err != nil {
		return scheduling.Reservation{}, false, err
	}
	if err := expected.Validate(); err != nil {
		return scheduling.Reservation{}, false, domainWrap(ctx, err, faults.CodeInvalidArgument,
			"expected_version_invalid", "expected reservation version is invalid", operation)
	}
	if fence == 0 {
		return scheduling.Reservation{}, false, domainError(ctx, faults.CodeInvalidArgument,
			"lease_fence_invalid", "leadership fence is required", operation)
	}
	if now.IsZero() {
		return scheduling.Reservation{}, false, domainError(ctx, faults.CodeInvalidArgument,
			"snapshot_time_invalid", "transition time is required", operation)
	}
	transactionTime := now.Round(0).UTC()

	result, err := runMutation(ctx, store, operation, func(txContext context.Context) (transitionResult, error) {
		state, lockErr := store.lockLedger(txContext, operation)
		if lockErr != nil {
			return transitionResult{}, lockErr
		}
		if expireErr := store.expire(txContext, transactionTime, operation); expireErr != nil {
			return transitionResult{}, expireErr
		}
		if fence < state.fence {
			return transitionResult{}, domainError(txContext, faults.CodeConflict,
				"lease_fence_stale", "writer holds an older leadership fence than the store", operation)
		}
		current, found, lookupErr := store.lockReservation(txContext,
			`reservation_id=$1`, id.String(), operation)
		if lookupErr != nil {
			return transitionResult{}, lookupErr
		}
		if !found {
			return transitionResult{}, domainError(txContext, faults.CodeNotFound,
				"reservation_not_found", "reservation was not found", operation)
		}
		// Already in the target state is a replay of a write that landed, not a
		// conflict: the worker crashed between the commit and the ack. This is
		// checked before the version precondition on purpose, because a replay
		// carries the version from before the write it is replaying.
		if current.State == target {
			return transitionResult{reservation: current, replayed: true}, nil
		}
		if current.State.Terminal() {
			return transitionResult{}, domainError(txContext, faults.CodeFailedPrecondition,
				"reservation_terminal", "reservation is already terminal", operation)
		}
		if current.Version.String() != expected.String() {
			return transitionResult{}, domainError(txContext, faults.CodeConflict,
				"reservation_version_stale", "reservation version is stale", operation)
		}
		updated, applyErr := applyTransition(txContext, current, target, assignment, fence, transactionTime, operation)
		if applyErr != nil {
			return transitionResult{}, applyErr
		}
		if writeErr := store.updateReservation(txContext, updated, transactionTime, operation); writeErr != nil {
			return transitionResult{}, writeErr
		}
		if emitErr := store.emitReservation(txContext,
			"scheduling.reservation."+string(target), updated); emitErr != nil {
			return transitionResult{}, emitErr
		}
		if ledgerErr := store.advanceLedger(txContext, fence, transactionTime, operation); ledgerErr != nil {
			return transitionResult{}, ledgerErr
		}
		return transitionResult{reservation: updated}, nil
	})
	if err != nil {
		return scheduling.Reservation{}, false, err
	}
	return result.reservation, result.replayed, nil
}

// applyTransition dispatches to the domain's own sealing methods. Preemption is
// absent: it is authored by a plan rather than by an identifier, so it goes
// through Preempt in preemption.go and never reaches here.
func applyTransition(
	ctx context.Context, current scheduling.Reservation, target scheduling.ReservationState,
	assignment scheduling.TopologyAssignment, fence uint64, at time.Time, operation string,
) (scheduling.Reservation, error) {
	switch target {
	case scheduling.ReservationBound:
		return current.Bind(at, assignment, fence)
	case scheduling.ReservationCompleted:
		return current.Complete(at, fence)
	case scheduling.ReservationReleased:
		return current.Release(at, fence)
	case scheduling.ReservationExpired:
		return current.Expire(at, fence)
	default:
		return scheduling.Reservation{}, domainError(ctx, faults.CodeInvalidArgument,
			"reservation_transition_invalid", "reservation transition is invalid", operation)
	}
}

// lockReservation reads one reservation under a row lock. The predicate is a
// compiled-in fragment naming one indexed column and the value always reaches
// the server as a bound parameter, so no caller-supplied string is ever read as
// SQL.
func (store *Store) lockReservation(ctx context.Context, predicate string, value any, operation string) (scheduling.Reservation, bool, error) {
	var document []byte
	err := store.executor(ctx).QueryRowContext(ctx,
		fmt.Sprintf(`SELECT document FROM %s WHERE %s FOR UPDATE`, store.reservations, predicate),
		value).Scan(&document)
	if errors.Is(err, sql.ErrNoRows) {
		return scheduling.Reservation{}, false, nil
	}
	if err != nil {
		return scheduling.Reservation{}, false, provider(ctx, err, operation+".Lock")
	}
	decoded, decodeErr := decodeDocument[reservationDocument](ctx, document, operation)
	if decodeErr != nil {
		return scheduling.Reservation{}, false, decodeErr
	}
	return scheduling.Reservation(decoded), true, nil
}

func (store *Store) insertReservation(ctx context.Context, record scheduling.Reservation, key string, at time.Time, operation string) error {
	arguments, err := reservationArguments(ctx, record, key, at, operation)
	if err != nil {
		return err
	}
	query := fmt.Sprintf(`INSERT INTO %s (%s) VALUES (%s)`,
		store.reservations, reservationColumns(), placeholders(len(arguments)))
	if _, execErr := store.executor(ctx).ExecContext(ctx, query, arguments...); execErr != nil {
		return provider(ctx, execErr, operation)
	}
	return nil
}

// updateReservation writes back a transitioned record. Only the mutable
// projection is assigned: the placement is immutable, so its key, domain,
// tenant, run coordinates, window, and demand columns cannot change, and
// re-writing them would give a corrupted document a second chance to move them.
func (store *Store) updateReservation(ctx context.Context, record scheduling.Reservation, at time.Time, operation string) error {
	document, err := marshalDocument(ctx, record, operation)
	if err != nil {
		return err
	}
	fence, err := sqlUint(ctx, record.LeaseFence, "lease_fence", operation)
	if err != nil {
		return err
	}
	generation, err := sqlUint(ctx, record.Version.Generation(), "resource_generation", operation)
	if err != nil {
		return err
	}
	query := fmt.Sprintf(`UPDATE %s SET
state=$2,lease_fence=$3,sequence=$4,bound_at=$5,finalized_at=$6,preemptor_id=$7,
resource_version=$8,resource_generation=$9,document=$10::jsonb,written_at=$11
WHERE reservation_id=$1`, store.reservations)
	if _, execErr := store.executor(ctx).ExecContext(ctx, query,
		record.ID.String(), string(record.State), fence, int64(record.Sequence),
		record.BoundAt.Round(0).UTC(), record.FinalizedAt.Round(0).UTC(),
		nullableID(record.Preemptor), record.Version.String(), generation,
		document, at.Round(0).UTC(),
	); execErr != nil {
		return provider(ctx, execErr, operation)
	}
	return nil
}

func reservationArguments(ctx context.Context, record scheduling.Reservation, key string, at time.Time, operation string) ([]any, error) {
	document, err := marshalDocument(ctx, record, operation)
	if err != nil {
		return nil, err
	}
	fence, err := sqlUint(ctx, record.LeaseFence, "lease_fence", operation)
	if err != nil {
		return nil, err
	}
	generation, err := sqlUint(ctx, record.Version.Generation(), "resource_generation", operation)
	if err != nil {
		return nil, err
	}
	amounts, err := demandAmounts(ctx, record.Placement.Total, operation)
	if err != nil {
		return nil, err
	}
	arguments := []any{
		record.ID.String(), key, string(record.Placement.Pool.Domain.WorkloadClass()),
		record.Placement.Tenant, record.Placement.RunID, record.Placement.StageID,
		int64(record.Placement.Attempt), string(record.State), fence, int64(record.Sequence),
		record.CreatedAt.Round(0).UTC(), record.ExpiresAt.Round(0).UTC(),
		record.BoundAt.Round(0).UTC(), record.FinalizedAt.Round(0).UTC(),
		nullableID(record.Preemptor), record.Version.String(), generation,
	}
	arguments = append(arguments, amounts...)
	return append(arguments, document, at.Round(0).UTC()), nil
}

// nullableID renders a zero identifier as SQL NULL. identifiers.ID marshals its
// zero value to JSON null, and the column's CHECK compares the two with IS NOT
// DISTINCT FROM, so an empty string here would fail every write of a
// reservation that names no preemptor -- which is every reservation but one
// state.
func nullableID(id identifiers.ID) any {
	if id.IsZero() {
		return nil
	}
	return id.String()
}

func scanReservations(ctx context.Context, rows *sql.Rows, operation string) ([]scheduling.Reservation, error) {
	defer func() { _ = rows.Close() }()
	records := make([]scheduling.Reservation, 0, 16)
	for rows.Next() {
		var document []byte
		if err := rows.Scan(&document); err != nil {
			return nil, provider(ctx, err, operation)
		}
		decoded, err := decodeDocument[reservationDocument](ctx, document, operation)
		if err != nil {
			return nil, err
		}
		records = append(records, scheduling.Reservation(decoded))
	}
	if err := rows.Err(); err != nil {
		return nil, provider(ctx, err, operation)
	}
	return records, nil
}

// validateReservationID restates control/scheduling's two-line identifier rule.
//
// The domain's validateID is unexported and there is no exported wrapper that
// raises this exact reason, so the check is repeated here rather than
// approximated: a caller that switches on reservation_id_invalid must get the
// same answer from either adapter. The rule is small enough that restating it
// carries no drift risk -- unlike the tenant-name alphabet, which ledger.go
// borrows from the domain rather than copying.
func validateReservationID(ctx context.Context, id identifiers.ID, operation string) error {
	err := id.Validate()
	if err == nil && id.Kind().String() == "reservation" {
		return nil
	}
	return domainWrap(ctx, err, faults.CodeInvalidArgument,
		"reservation_id_invalid", "reservation_id is invalid", operation)
}
