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
// commits once, refuses to nest, and takes the singleton ledger lock as its
// first statement.
type harness struct {
	store  *Store
	state  *sqltest.State
	mutex  sync.Mutex
	script []string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	rig := &harness{state: &sqltest.State{}}
	rig.state.Exec = func(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
		rig.record(query)
		return driver.RowsAffected(1), nil
	}
	rig.state.Query = func(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
		rig.record(query)
		return scriptedRows(query), nil
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
	store, err := New(database, audit.NopRecorder{}, messages,
		WithClock(clock.RealClock{}), WithRetry(retries))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	rig.store = store
	return rig
}

func (rig *harness) record(query string) {
	rig.mutex.Lock()
	defer rig.mutex.Unlock()
	rig.script = append(rig.script, query)
}

func (rig *harness) statements() []string {
	rig.mutex.Lock()
	defer rig.mutex.Unlock()
	return append([]string(nil), rig.script...)
}

func (rig *harness) reset() {
	rig.mutex.Lock()
	defer rig.mutex.Unlock()
	rig.script = nil
	rig.state.Begins.Store(0)
	rig.state.Commits.Store(0)
	rig.state.Rollbacks.Store(0)
	rig.state.Queries.Store(0)
	rig.state.Executions.Store(0)
}

// scriptedRows answers the handful of shapes the transaction-shape tests need.
// The ledger row is the only one that must exist: every mutation locks it
// first, so a store that could not read it would fail before reaching anything
// this file is trying to observe.
func scriptedRows(query string) driver.Rows {
	switch {
	case strings.Contains(query, "FROM "+DefaultLedgerTable):
		return sqltest.NewRows([]string{"fence", "epoch"}, []driver.Value{int64(0), int64(1)})
	case strings.Contains(query, "count(*)"):
		return sqltest.NewRows([]string{"total"}, []driver.Value{int64(0)})
	default:
		return sqltest.NewRows([]string{"document"})
	}
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
			first := statements[0]
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
	rig.state.Query = func(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
		rig.record(query)
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
	rig.state.Exec = func(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
		rig.record(query)
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

// The version the domain reads back is the version the domain sealed. This is
// the whole of divergence two: there is no restated digest in this package, so
// the property to pin is that a transition applied through the store is the one
// the reference adapter would have produced.
func TestTransitionsAreSealedByTheDomainNotByThisPackage(t *testing.T) {
	reservation := testReservation(t)
	bound, err := reservation.Bind(testStart.Add(time.Minute), scheduling.TopologyAssignment{}, testFence)
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if bound.Version.String() == reservation.Version.String() {
		t.Fatal("a transition must advance the resource version")
	}
	if bound.Version.Generation() != uint64(bound.Sequence)+1 {
		t.Fatalf("generation %d does not match sequence %d", bound.Version.Generation(), bound.Sequence)
	}
	// resource_generation is a projected column with a CHECK against this
	// relationship, so the two have to agree or no bound reservation is
	// storable at all.
	if err := bound.Validate(); err != nil {
		t.Fatalf("a domain-sealed transition must validate: %v", err)
	}
}

var _ outbox.Store = (*outboxmemory.Store)(nil)
