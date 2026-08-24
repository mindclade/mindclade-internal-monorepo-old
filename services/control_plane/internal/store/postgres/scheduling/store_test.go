// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package schedulingpostgres

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"go.mindclade.dev/control/orchestration"
	"go.mindclade.dev/control/scheduling"
	"go.mindclade.dev/libs/go/audit"
	"go.mindclade.dev/libs/go/auth"
	"go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/coordination/outbox"
	outboxmemory "go.mindclade.dev/libs/go/coordination/outbox/memory"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/retry"
	"go.mindclade.dev/libs/go/storage/sql/sqltest"
	"go.mindclade.dev/libs/go/storage/sql/transaction"
)

var testStart = time.Date(2026, time.August, 23, 6, 0, 0, 0, time.UTC)

const (
	gibibyte  = uint64(1) << 30
	tebibyte  = uint64(1) << 40
	testFence = uint64(7)
)

// harness is the fake-driver rig. The driver is not a SQL emulator: it cannot
// check a CHECK constraint, a FOR UPDATE, or an aggregate, which is what
// live_postgres_test.go is for. What it proves here is the part a real server
// would happily let through -- whether a mutation opens one transaction,
// commits once, refuses to nest, takes the singleton ledger lock as its first
// statement, and writes back exactly what the domain sealed.
//
// It records the ordered statement stream WITH its bound arguments, and it
// captures every audit event, so a test can assert on what the store actually
// sent rather than only on what it returned.
type harness struct {
	store  *Store
	state  *sqltest.State
	mutex  sync.Mutex
	script []statement
	audits []audit.Event
	// reservation is the document the scripted reservation lock and expiry
	// sweep return. Nil means "no such row". A reservation UPDATE the store
	// issues replaces it, so a read after a write sees what was written -- the
	// driver cannot execute SQL, but it must not answer with a row the store
	// just overwrote.
	reservation []byte
	// suppressSweep makes the expiry sweep find nothing even for an eligible
	// row. That is not a lie about SQL: the sweep is bounded at
	// MaximumExpirySweep and ordered by deadline, so a lapsed hold outside the
	// oldest batch is exactly this state, and it is the only way the explicit
	// Expire transition is reachable rather than replaying the sweep's work.
	suppressSweep bool
}

type statement struct {
	query string
	args  []driver.NamedValue
}

// recordingRecorder captures the audit events the store emits so a test can
// assert who was recorded as their author.
type recordingRecorder struct{ rig *harness }

func (recorder recordingRecorder) Record(_ context.Context, event audit.Event) error {
	recorder.rig.mutex.Lock()
	defer recorder.rig.mutex.Unlock()
	recorder.rig.audits = append(recorder.rig.audits, event)
	return nil
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	rig := &harness{state: &sqltest.State{}}
	rig.state.Exec = func(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
		rig.record(query, args)
		return driver.RowsAffected(1), nil
	}
	rig.state.Query = func(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
		rig.record(query, args)
		return rig.rows(query, args), nil
	}
	database, err := sqltest.Open(rig.state)
	if err != nil {
		t.Fatalf("open fake database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	messages, err := outboxmemory.New()
	if err != nil {
		t.Fatalf("outbox: %v", err)
	}
	// A real clock, deliberately. The store's retry executor sleeps between
	// serialization retries, and a fake clock that nothing advances makes that
	// sleep block forever -- the test would hang rather than fail, which is the
	// worst way for an assertion to be wrong. Backoff is collapsed to nothing so
	// the retry path stays fast without becoming untested.
	policy, err := retry.NewPolicy(
		retry.WithMaxAttempts(2),
		retry.WithBackoff(retry.ImmediateBackoff()),
		retry.WithoutJitter(),
	)
	if err != nil {
		t.Fatalf("retry policy: %v", err)
	}
	retries, err := retry.NewExecutor(policy)
	if err != nil {
		t.Fatalf("retry executor: %v", err)
	}
	store, err := New(database, recordingRecorder{rig: rig}, messages,
		WithClock(clock.RealClock{}), WithRetry(retries))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	rig.store = store
	return rig
}

func (rig *harness) record(query string, args []driver.NamedValue) {
	rig.mutex.Lock()
	defer rig.mutex.Unlock()
	rig.script = append(rig.script, statement{query: query, args: append([]driver.NamedValue(nil), args...)})
	// Reflect a reservation write back into the scripted row. Without this the
	// harness would keep serving the pre-transition document, and every
	// read-after-write assertion -- including the replay path, which is how a
	// re-driven mutation is supposed to behave -- would be tested against a
	// state the store had already replaced.
	if !strings.HasPrefix(query, "UPDATE "+DefaultReservationTable+" SET") {
		return
	}
	for _, arg := range args {
		if arg.Ordinal != reservationDocumentOrdinal {
			continue
		}
		if document, ok := arg.Value.([]byte); ok {
			rig.reservation = append([]byte(nil), document...)
		}
	}
}

// reservationDocumentOrdinal is the document parameter of updateReservation's
// UPDATE. It is named once so the harness and the column assertions below
// cannot disagree about which placeholder carries the record.
const reservationDocumentOrdinal = 10

func (rig *harness) statements() []statement {
	rig.mutex.Lock()
	defer rig.mutex.Unlock()
	return append([]statement(nil), rig.script...)
}

func (rig *harness) recordedAudits() []audit.Event {
	rig.mutex.Lock()
	defer rig.mutex.Unlock()
	return append([]audit.Event(nil), rig.audits...)
}

// store scripts the document the reservation lock and the expiry sweep return.
func (rig *harness) storeReservation(t *testing.T, reservation scheduling.Reservation) {
	t.Helper()
	document, err := json.Marshal(reservation)
	if err != nil {
		t.Fatalf("marshal reservation: %v", err)
	}
	rig.mutex.Lock()
	defer rig.mutex.Unlock()
	rig.reservation = document
}

func (rig *harness) reset() {
	rig.mutex.Lock()
	defer rig.mutex.Unlock()
	rig.script = nil
	rig.audits = nil
	rig.state.Begins.Store(0)
	rig.state.Commits.Store(0)
	rig.state.Rollbacks.Store(0)
	rig.state.Queries.Store(0)
	rig.state.Executions.Store(0)
}

// rows answers the handful of shapes the transaction-shape tests need.
//
// The ledger row is the only one that must always exist: every mutation locks
// it first, so a store that could not read it would fail before reaching
// anything these tests are trying to observe. The reservation lock and the
// expiry sweep are matched on fragments unique to each -- `WHERE
// reservation_id=` and `ORDER BY expires_at` -- so a test can put a document in
// front of one without also feeding it to the other.
func (rig *harness) rows(query string, args []driver.NamedValue) driver.Rows {
	rig.mutex.Lock()
	document, suppressed := rig.reservation, rig.suppressSweep
	rig.mutex.Unlock()
	switch {
	case strings.Contains(query, "FROM "+DefaultLedgerTable):
		return sqltest.NewRows([]string{"fence", "epoch"}, []driver.Value{int64(0), int64(1)})
	case strings.Contains(query, "count(*)"):
		return sqltest.NewRows([]string{"total"}, []driver.Value{int64(0)})
	case document != nil && strings.Contains(query, "WHERE reservation_id="):
		return sqltest.NewRows([]string{"document"}, []driver.Value{document})
	case document != nil && strings.Contains(query, "ORDER BY expires_at"):
		// The sweep's own predicate, honoured. Handing it a row that
		// `state='held' AND expires_at <= $2` would not have selected makes the
		// store attempt a transition the domain refuses, so the harness would
		// be manufacturing a failure a real server cannot produce.
		if suppressed || !sweptByDeadline(document, args) {
			return sqltest.NewRows([]string{"document"})
		}
		return sqltest.NewRows([]string{"document"}, []driver.Value{document})
	default:
		return sqltest.NewRows([]string{"document"})
	}
}

// sweptByDeadline evaluates `state='held' AND expires_at <= $2` against the
// scripted document.
func sweptByDeadline(document []byte, args []driver.NamedValue) bool {
	var record scheduling.Reservation
	if err := json.Unmarshal(document, &record); err != nil {
		return false
	}
	if record.State != scheduling.ReservationHeld {
		return false
	}
	for _, arg := range args {
		if arg.Ordinal != 2 {
			continue
		}
		deadline, ok := arg.Value.(time.Time)
		return ok && !deadline.Before(record.ExpiresAt)
	}
	return false
}

// reservationUpdate returns the single UPDATE this mutation issued against the
// reservation table, so a test can read the columns the store actually wrote
// rather than trusting the value it handed back.
func (rig *harness) reservationUpdate(t *testing.T) statement {
	t.Helper()
	found := make([]statement, 0, 1)
	for _, entry := range rig.statements() {
		if strings.HasPrefix(entry.query, "UPDATE "+DefaultReservationTable+" SET") {
			found = append(found, entry)
		}
	}
	if len(found) != 1 {
		t.Fatalf("expected exactly one reservation UPDATE, found %d", len(found))
	}
	return found[0]
}

func (entry statement) argument(t *testing.T, ordinal int) driver.Value {
	t.Helper()
	for _, arg := range entry.args {
		if int(arg.Ordinal) == ordinal {
			return arg.Value
		}
	}
	t.Fatalf("statement has no argument $%d: %s", ordinal, entry.query)
	return nil
}

// testPrincipal is an authenticated caller for the authorship tests.
func testPrincipal(t *testing.T) auth.Principal {
	t.Helper()
	principal, err := auth.NewPrincipal(auth.PrincipalKindUser, "operator",
		auth.WithIssuer("mindclade"))
	if err != nil {
		t.Fatalf("new principal: %v", err)
	}
	return principal
}

func testPrincipalContext(t *testing.T) context.Context {
	t.Helper()
	ctx, err := auth.WithPrincipal(context.Background(), testPrincipal(t))
	if err != nil {
		t.Fatalf("context with principal: %v", err)
	}
	return ctx
}

func testID(t *testing.T, kind string) string {
	t.Helper()
	id, err := identifiers.NewID(identifiers.MustParseKind(kind))
	if err != nil {
		t.Fatalf("new %s id: %v", kind, err)
	}
	return id.String()
}

func testReservationID(t *testing.T) identifiers.ID {
	t.Helper()
	id, err := identifiers.NewID(identifiers.MustParseKind("reservation"))
	if err != nil {
		t.Fatalf("new reservation id: %v", err)
	}
	return id
}

func testDomain(t *testing.T, class scheduling.WorkloadClass) scheduling.CapacityDomain {
	t.Helper()
	domain, err := scheduling.DomainFor(class)
	if err != nil {
		t.Fatalf("domain %s: %v", class, err)
	}
	return domain
}

func cpuDemand(cpu, memory, storage, pods uint64) scheduling.Demand {
	return scheduling.Demand{
		scheduling.ResourceCPU:              cpu,
		scheduling.ResourceMemory:           memory,
		scheduling.ResourceEphemeralStorage: storage,
		scheduling.ResourcePods:             pods,
	}
}

// testSnapshot is a one-domain fleet with room to spare. It is built as a value
// rather than read from a store, which is exactly how the domain intends a
// snapshot to be used: admission is a pure function of it.
func testSnapshot(t *testing.T) scheduling.FleetSnapshot {
	t.Helper()
	domain := testDomain(t, scheduling.WorkloadClassBatchCPU)
	nominal := cpuDemand(64_000, 256*gibibyte, tebibyte, 128)
	snapshot := scheduling.FleetSnapshot{
		Epoch:      1,
		ObservedAt: testStart,
		Allocatables: []scheduling.Allocatable{
			{Domain: domain, Nominal: nominal.Clone(), Reserved: make(scheduling.Demand)},
		},
		Shares: []scheduling.FairShare{
			{Domain: domain, Capacity: nominal.Clone(), Claims: []scheduling.ShareClaim{
				{Tenant: "research", Weight: 1, Used: make(scheduling.Demand)},
			}},
		},
		TopologyDigest: scheduling.TopologyFingerprint(),
	}
	if err := snapshot.Validate(); err != nil {
		t.Fatalf("fixture snapshot is invalid: %v", err)
	}
	return snapshot
}

// testReservation seals a held reservation the way the domain does: a placement
// decided against a snapshot, then NewReservation. A hand-built literal is not
// a valid Reservation -- Validate re-derives the digest that seals it.
func testReservation(t *testing.T) scheduling.Reservation {
	t.Helper()
	snapshot := testSnapshot(t)
	request := scheduling.PlacementRequest{
		Admission: scheduling.AdmissionRequest{
			WorkloadID:  testWorkloadID(t),
			Tenant:      "research",
			Workspace:   "research-team",
			StageKind:   orchestration.StagePreprocess,
			Pool:        scheduling.PoolFeaturizationCPU,
			Accelerator: scheduling.AcceleratorNone,
			Priority:    scheduling.PriorityBatch,
			Demand:      cpuDemand(2_000, 4*gibibyte, 8*gibibyte, 1),
			Replicas:    2,
		},
		RunID:   testID(t, "run"),
		StageID: testID(t, "stage"),
		Attempt: 1,
	}
	placement, err := snapshot.Place(request, testStart)
	if err != nil {
		t.Fatalf("place: %v", err)
	}
	reservation, err := scheduling.NewReservation(
		testReservationID(t), placement, testFence, scheduling.DefaultReservationTTL)
	if err != nil {
		t.Fatalf("new reservation: %v", err)
	}
	return reservation
}

func testWorkloadID(t *testing.T) identifiers.ID {
	t.Helper()
	id, err := identifiers.NewID(identifiers.MustParseKind("workload"))
	if err != nil {
		t.Fatalf("new workload id: %v", err)
	}
	return id
}

// A mutation must own exactly one transaction. Two would mean a partial commit
// is reachable: the domain write could land while the audit record and the
// outbox message did not, which is the failure the single-transaction rule
// exists to prevent.
func TestMutationOpensExactlyOneTransaction(t *testing.T) {
	rig := newHarness(t)
	if err := rig.store.PutWeight(context.Background(), "research", 1); err != nil {
		t.Fatalf("PutWeight: %v", err)
	}
	if begins := rig.state.Begins.Load(); begins != 1 {
		t.Fatalf("begins = %d, want exactly 1", begins)
	}
	if commits := rig.state.Commits.Load(); commits != 1 {
		t.Fatalf("commits = %d, want exactly 1", commits)
	}
	if rollbacks := rig.state.Rollbacks.Load(); rollbacks != 0 {
		t.Fatalf("rollbacks = %d, want 0", rollbacks)
	}
}

// A failed write must leave nothing behind. Committing a domain row whose audit
// record or outbox message failed is precisely the split this store is built to
// make impossible.
func TestAFailedWriteRollsBack(t *testing.T) {
	rig := newHarness(t)
	rig.state.Exec = func(context.Context, string, []driver.NamedValue) (driver.Result, error) {
		return nil, errors.New("write failed")
	}
	if err := rig.store.PutWeight(context.Background(), "research", 1); err == nil {
		t.Fatal("a failing write must surface as an error")
	}
	if rig.state.Commits.Load() != 0 {
		t.Fatal("a failed write must not commit")
	}
	if rig.state.Rollbacks.Load() == 0 {
		t.Fatal("a failed write must roll back")
	}
}

// The store owns its serializable transaction, so joining a caller's is refused
// rather than silently downgraded to whatever isolation the outer one chose.
func TestNestedTransactionIsRefused(t *testing.T) {
	rig := newHarness(t)
	outer, err := sqltest.Open(&sqltest.State{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = outer.Close() }()
	tx, err := outer.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	ctx, err := transaction.ContextWithTx(context.Background(), tx)
	if err != nil {
		t.Fatalf("context: %v", err)
	}
	// Snapshot is in this list on purpose. It reads like a query, but it
	// re-seals expired holds before it reads the ledger, so it is a mutation
	// and must refuse an ambient transaction exactly as Reserve does.
	cases := map[string]func() error{
		"Snapshot": func() error {
			_, err := rig.store.Snapshot(ctx, testStart)
			return err
		},
		"Held": func() error {
			_, err := rig.store.Held(ctx, testDomain(t, scheduling.WorkloadClassBatchCPU), testStart)
			return err
		},
		"PutWeight": func() error { return rig.store.PutWeight(ctx, "research", 1) },
		"Reserve": func() error {
			_, _, err := rig.store.Reserve(ctx, testSnapshot(t), testReservation(t), testStart)
			return err
		},
	}
	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			err := call()
			if err == nil {
				t.Fatal("a nested transaction must be refused")
			}
			if !faults.IsCode(err, faults.CodeFailedPrecondition) {
				t.Fatalf("code = %s, want failed_precondition", faults.CodeOf(err))
			}
		})
	}
}

// Divergence one, stated as a test: Snapshot and Held are writes because both
// re-seal expired holds before reading the ledger. Get is the only pure read in
// the package -- it names one reservation and has no ledger to expire.
func TestSnapshotAndHeldRunAsMutationsWhileGetDoesNot(t *testing.T) {
	rig := newHarness(t)
	ctx := context.Background()

	if _, err := rig.store.Snapshot(ctx, testStart); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if begins := rig.state.Begins.Load(); begins != 1 {
		t.Fatalf("Snapshot begins = %d, want 1; an expiring read is a write", begins)
	}
	if commits := rig.state.Commits.Load(); commits != 1 {
		t.Fatalf("Snapshot commits = %d, want 1", commits)
	}

	rig.reset()
	if _, err := rig.store.Held(ctx, testDomain(t, scheduling.WorkloadClassBatchCPU), testStart); err != nil {
		t.Fatalf("Held: %v", err)
	}
	if begins := rig.state.Begins.Load(); begins != 1 {
		t.Fatalf("Held begins = %d, want 1; an expiring read is a write", begins)
	}

	rig.reset()
	_, err := rig.store.Get(ctx, testReservationID(t))
	if err == nil {
		t.Fatal("a reservation that is not stored must not be found")
	}
	if !faults.IsCode(err, faults.CodeNotFound) {
		t.Fatalf("code = %s, want not_found", faults.CodeOf(err))
	}
	if begins := rig.state.Begins.Load(); begins != 0 {
		t.Fatalf("Get begins = %d, want 0; Get is a pure read", begins)
	}
}

// Divergence four, stated as a test: the singleton ledger row is locked first
// by every mutation. A mutation that reached any other row before taking that
// lock would order two writers differently depending on which rows they touch,
// which is how a hot row becomes a deadlock instead of a queue.
func TestEveryMutationLocksTheLedgerRowFirst(t *testing.T) {
	rig := newHarness(t)
	ctx := context.Background()
	domain := testDomain(t, scheduling.WorkloadClassBatchCPU)
	reservation := testReservation(t)

	cases := map[string]func(){
		"Snapshot":  func() { _, _ = rig.store.Snapshot(ctx, testStart) },
		"Held":      func() { _, _ = rig.store.Held(ctx, domain, testStart) },
		"PutQuota":  func() { _ = rig.store.PutQuota(ctx, domain, cpuDemand(1_000, gibibyte, gibibyte, 1)) },
		"PutWeight": func() { _ = rig.store.PutWeight(ctx, "research", 1) },
		"Reserve": func() {
			_, _, _ = rig.store.Reserve(ctx, testSnapshot(t), reservation, testStart)
		},
		"Bind": func() {
			_, _, _ = rig.store.Bind(ctx, reservation.ID, reservation.Version,
				scheduling.TopologyAssignment{}, testFence, testStart)
		},
		"Complete": func() {
			_, _, _ = rig.store.Complete(ctx, reservation.ID, reservation.Version, testFence, testStart)
		},
		"Release": func() {
			_, _, _ = rig.store.Release(ctx, reservation.ID, reservation.Version, testFence, testStart)
		},
		"Expire": func() {
			_, _, _ = rig.store.Expire(ctx, reservation.ID, reservation.Version, testFence, testStart)
		},
		"Preempt": func() { _, _, _ = rig.store.Preempt(ctx, testPlan(t, reservation), testFence, testStart) },
	}
	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			rig.reset()
			call()
			statements := rig.statements()
			if len(statements) == 0 {
				t.Fatal("the mutation never reached the database")
			}
			first := statements[0].query
			if !strings.Contains(first, DefaultLedgerTable) || !strings.Contains(first, "FOR UPDATE") {
				t.Fatalf("first statement was not the ledger lock:\n%s", first)
			}
		})
	}
}

func testPlan(t *testing.T, victim scheduling.Reservation) scheduling.PreemptionPlan {
	t.Helper()
	plan := scheduling.PreemptionPlan{
		Candidate: testReservationID(t),
		Domain:    testDomain(t, scheduling.WorkloadClassBatchCPU),
		Shortfall: victim.Placement.Total.Clone(),
		Reclaimed: victim.Placement.Total.Clone(),
		Victims: []scheduling.Victim{{
			ReservationID: victim.ID,
			Tenant:        victim.Placement.Tenant,
			Priority:      victim.Placement.Priority,
			Reclaimed:     victim.Placement.Total.Clone(),
			Action:        scheduling.ActionEvictAndRequeue,
		}},
	}
	if err := plan.Validate(); err != nil {
		t.Fatalf("fixture plan is invalid: %v", err)
	}
	return plan
}

// sqlStateError is what lib/pq returns for a server error. The store classifies
// on it, so the tests below have to produce one rather than a bare error.
type sqlStateError struct {
	state   string
	message string
}

func (err sqlStateError) Error() string    { return err.message }
func (err sqlStateError) SQLState() string { return err.state }

// A serialization failure is contention, not a fault: the transaction has to be
// replayed. This store re-wraps the statement-level 40001 as well as the
// commit-level one, because the ledger lock is the first statement of every
// mutation and is where the loser usually learns it lost.
func TestSerializationFailureIsRetried(t *testing.T) {
	rig := newHarness(t)
	rig.state.Query = func(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
		rig.record(query, args)
		return nil, sqlStateError{state: "40001", message: "could not serialize access"}
	}
	if err := rig.store.PutWeight(context.Background(), "research", 1); err == nil {
		t.Fatal("an exhausted retry budget must surface as an error")
	}
	// The harness policy allows two attempts, so a retried mutation begins
	// twice. One would mean the 40001 was treated as terminal.
	if begins := rig.state.Begins.Load(); begins != 2 {
		t.Fatalf("begins = %d, want 2; a serialization failure must be replayed", begins)
	}
}

// A constraint violation is not contention and can never succeed on a replay.
// Retrying it would turn a permanent write rejection into a stall.
func TestConstraintViolationIsNotRetried(t *testing.T) {
	rig := newHarness(t)
	rig.state.Exec = func(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
		rig.record(query, args)
		return nil, sqlStateError{state: "23514", message: "check constraint violated"}
	}
	if err := rig.store.PutWeight(context.Background(), "research", 1); err == nil {
		t.Fatal("a constraint violation must surface as an error")
	}
	if begins := rig.state.Begins.Load(); begins != 1 {
		t.Fatalf("begins = %d, want 1; a constraint violation must not be replayed", begins)
	}
}

// A document that no longer validates must not be handed to a scheduler as if
// it were sound. Reservation.Validate re-derives the digest that seals the
// record, so a field edited out of band cannot survive this.
func TestDecodeRejectsATamperedDocument(t *testing.T) {
	reservation := testReservation(t)
	document, err := json.Marshal(reservation)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	tampered := strings.Replace(string(document), `"lease_fence":7`, `"lease_fence":9`, 1)
	if tampered == string(document) {
		t.Fatal("the test fixture did not change the stored fence")
	}
	if _, err := decodeDocument[reservationDocument](context.Background(), []byte(tampered), "test"); err == nil {
		t.Fatal("a reservation whose version no longer seals its content must be rejected")
	}
}

func TestStoreRequiresItsProviders(t *testing.T) {
	database, err := sqltest.Open(&sqltest.State{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = database.Close() }()
	messages, err := outboxmemory.New()
	if err != nil {
		t.Fatalf("outbox: %v", err)
	}
	cases := map[string]func() (*Store, error){
		"no database": func() (*Store, error) {
			return New(nil, audit.NopRecorder{}, messages)
		},
		"no recorder": func() (*Store, error) {
			return New(database, nil, messages)
		},
		"no outbox": func() (*Store, error) {
			return New(database, audit.NopRecorder{}, nil)
		},
		"invalid table": func() (*Store, error) {
			return New(database, audit.NopRecorder{}, messages,
				WithTables("valid", "also_valid", "still valid?", "ledger"))
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := build(); err == nil {
				t.Fatal("a store missing or misconfigured must fail construction")
			}
		})
	}
}

// The schema is four tables, and task 2's migration set is sized from that
// count. A fifth table added here without a migration would be a schema this
// store writes to and no deployment creates.
func TestDDLProducesTheFourSchemas(t *testing.T) {
	statements, err := DDL("res", "quo", "wei", "led")
	if err != nil {
		t.Fatalf("DDL: %v", err)
	}
	if len(statements) != 4 {
		t.Fatalf("statements = %d, want 4", len(statements))
	}
	for index, table := range []string{"res", "quo", "wei", "led"} {
		if !strings.Contains(statements[index], "CREATE TABLE IF NOT EXISTS "+table+" (") {
			t.Fatalf("statement %d does not create %s:\n%s", index, table, statements[index])
		}
	}
	// The ledger row is schema, not data: an epoch of zero is not a valid
	// FleetSnapshot epoch, so the row has to exist before anything reads it.
	if !strings.Contains(statements[3], "INSERT INTO led") ||
		!strings.Contains(statements[3], "ON CONFLICT (singleton) DO NOTHING") {
		t.Fatalf("the ledger schema does not seed its singleton row:\n%s", statements[3])
	}
	if _, err := DDL("res", "quo", "wei", "not a table"); err == nil {
		t.Fatal("an invalid table name must be refused")
	}
	// DDL and WithTables must apply the same rule to the same bytes. DDL used
	// to validate the trimmed name while interpolating the untrimmed one, which
	// made this pair disagree: a schema you could create and then not point the
	// store at.
	padded := " res "
	_, ddlErr := DDL(padded, "quo", "wei", "led")
	optionErr := WithTables(padded, "quo", "wei", "led")(&Store{})
	if (ddlErr == nil) != (optionErr == nil) {
		t.Fatalf("DDL and WithTables disagree on %q: DDL=%v WithTables=%v", padded, ddlErr, optionErr)
	}
}

// The projection has to cover the whole ResourceGroup. A resource the domain
// starts covering and this schema does not project would be summed as zero by
// the ledger -- capacity charged to nobody.
func TestDemandProjectionCoversTheWholeResourceGroup(t *testing.T) {
	projected := make(map[scheduling.ResourceName]struct{}, len(demandColumns))
	for _, column := range demandColumns {
		projected[column.resource] = struct{}{}
	}
	// The accelerated domain's group is the full five; the batch domain's is a
	// subset of it, so this covers every resource the schema can ever see.
	covered := testDomain(t, scheduling.WorkloadClassTrainingH100).CoveredResources()
	if len(covered) != len(projected) {
		t.Fatalf("projected %d resources, the ResourceGroup covers %d", len(projected), len(covered))
	}
	for _, name := range covered {
		if _, ok := projected[name]; !ok {
			t.Fatalf("resource %q is covered by the domain and not projected by the schema", name)
		}
	}
}

// Divergence two, pinned against THIS package rather than against the domain.
//
// The property is not "Reservation.Bind seals correctly" -- that is the domain's
// own test -- it is that the store persists the generation the domain sealed and
// restates nothing. So this drives store.Bind through the fake driver and reads
// the UPDATE the store actually issued: the returned value, the projected
// version and generation columns, and the stored document must all carry the
// version the domain produced for the identical transition. A store-side
// re-seal, a write-back of the pre-transition version, or a transition applied
// by anything other than Reservation.Bind fails here.
func TestTransitionsAreSealedByTheDomainNotByThisPackage(t *testing.T) {
	rig := newHarness(t)
	held := testReservation(t)
	rig.storeReservation(t, held)
	at := testStart.Add(time.Minute)

	// What the reference transition produces, computed independently of the
	// store from the same inputs the store is about to be handed.
	expected, err := held.Bind(at, scheduling.TopologyAssignment{}, testFence)
	if err != nil {
		t.Fatalf("domain Bind: %v", err)
	}
	if expected.Version.String() == held.Version.String() {
		t.Fatal("the fixture transition did not advance the version")
	}

	stored, replayed, err := rig.store.Bind(context.Background(), held.ID, held.Version,
		scheduling.TopologyAssignment{}, testFence, at)
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if replayed {
		t.Fatal("a first transition must not replay")
	}
	if stored.Version.String() != expected.Version.String() {
		t.Fatalf("returned version %s, want the domain's %s", stored.Version, expected.Version)
	}

	// The columns the store wrote, in the order updateReservation binds them:
	// $2 state, $4 sequence, $8 resource_version, $9 resource_generation,
	// $10 document.
	update := rig.reservationUpdate(t)
	if state := update.argument(t, 2); state != string(scheduling.ReservationBound) {
		t.Fatalf("state column = %v, want bound", state)
	}
	if sequence := update.argument(t, 4); sequence != int64(expected.Sequence) {
		t.Fatalf("sequence column = %v, want %d", sequence, expected.Sequence)
	}
	if version := update.argument(t, 8); version != expected.Version.String() {
		t.Fatalf("resource_version column = %v, want %s", version, expected.Version)
	}
	if generation := update.argument(t, 9); generation != int64(expected.Version.Generation()) {
		t.Fatalf("resource_generation column = %v, want %d", generation, expected.Version.Generation())
	}
	document, ok := update.argument(t, reservationDocumentOrdinal).([]byte)
	if !ok {
		t.Fatalf("document column is %T, want []byte", update.argument(t, reservationDocumentOrdinal))
	}
	written, err := decodeDocument[reservationDocument](context.Background(), document, "test")
	if err != nil {
		t.Fatalf("the stored document must revalidate: %v", err)
	}
	if scheduling.Reservation(written).Version.String() != expected.Version.String() {
		t.Fatalf("stored document version %s, want %s",
			scheduling.Reservation(written).Version, expected.Version)
	}
}

// Divergence one's audit consequence: the mutation that happens to run the
// expiry sweep is not the author of what it swept.
//
// An operator calling Snapshot with their principal on the context would
// otherwise be recorded as the author of every hold that lapsed while they were
// reading, which is the one way an audit log must not be wrong.
func TestSweptExpiriesAreAuthoredByTheSystemNotTheCaller(t *testing.T) {
	rig := newHarness(t)
	held := testReservation(t)
	rig.storeReservation(t, held)
	ctx := testPrincipalContext(t)

	// Past the hold's deadline, so the sweep re-seals it.
	if _, err := rig.store.Snapshot(ctx, held.ExpiresAt.Add(time.Second)); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	events := rig.recordedAudits()
	if len(events) != 1 {
		t.Fatalf("audit events = %d, want exactly 1 for one swept expiry", len(events))
	}
	actor := events[0].Actor()
	if actor.Kind() != auth.PrincipalKindSystem {
		t.Fatalf("actor kind = %s, want system; the caller did not cause this expiry", actor.Kind())
	}
	if actor.Subject() != "scheduling-service" {
		t.Fatalf("actor subject = %q, want the store's system actor", actor.Subject())
	}
	if subject := testPrincipal(t).Subject(); actor.Subject() == subject {
		t.Fatalf("the ambient principal %q was recorded as the author of a deadline-authored expiry", subject)
	}
}

// The other half of the same rule: a transition the caller actually asked for
// keeps the caller's principal. Forcing the system actor everywhere would erase
// who bound a reservation, which is exactly what the audit log is for.
func TestCallerAuthoredTransitionsKeepTheCallerPrincipal(t *testing.T) {
	rig := newHarness(t)
	held := testReservation(t)
	rig.storeReservation(t, held)
	ctx := testPrincipalContext(t)

	if _, _, err := rig.store.Bind(ctx, held.ID, held.Version,
		scheduling.TopologyAssignment{}, testFence, testStart.Add(time.Minute)); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	events := rig.recordedAudits()
	if len(events) != 1 {
		t.Fatalf("audit events = %d, want exactly 1", len(events))
	}
	actor := events[0].Actor()
	if actor.Kind() != auth.PrincipalKindUser || actor.Subject() != testPrincipal(t).Subject() {
		t.Fatalf("actor = %s/%s, want the calling principal", actor.Kind(), actor.Subject())
	}
}

// One logical event, one action name. The sweep and the explicit Expire both
// produce an expired reservation, and a consumer filtering on the action would
// have seen half the expiries when the two call sites named it differently.
// The action is now derived from the sealed record, so this compares the two
// paths rather than trusting that two string literals were kept in step.
func TestTheSweepAndTheExplicitExpireEmitOneActionName(t *testing.T) {
	held := testReservation(t)
	after := held.ExpiresAt.Add(time.Second)

	sweep := newHarness(t)
	sweep.storeReservation(t, held)
	if _, err := sweep.store.Snapshot(context.Background(), after); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	swept := sweep.recordedAudits()
	if len(swept) != 1 {
		t.Fatalf("sweep audit events = %d, want 1", len(swept))
	}

	// The sweep is suppressed so the explicit transition is the emitter. With a
	// small backlog the sweep in the same transaction reaches the row first and
	// the explicit call correctly replays, which would make this comparison
	// tautological; a lapsed hold outside the bounded batch is the real state
	// in which Expire does the work itself.
	explicit := newHarness(t)
	explicit.storeReservation(t, held)
	explicit.suppressSweep = true
	if _, _, err := explicit.store.Expire(context.Background(), held.ID, held.Version,
		testFence, after); err != nil {
		t.Fatalf("Expire: %v", err)
	}
	called := explicit.recordedAudits()
	if len(called) != 1 {
		t.Fatalf("explicit audit events = %d, want 1", len(called))
	}

	if swept[0].Action() != called[0].Action() {
		t.Fatalf("the sweep emits %q and the explicit transition emits %q; a consumer filtering on the action sees half the expiries",
			swept[0].Action(), called[0].Action())
	}
	if want := ReservationActionPrefix + string(scheduling.ReservationExpired); swept[0].Action().String() != want {
		t.Fatalf("action = %q, want %q derived from the sealed state", swept[0].Action(), want)
	}
}

var _ outbox.Store = (*outboxmemory.Store)(nil)
