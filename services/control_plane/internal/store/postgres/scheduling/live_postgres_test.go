// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Live PostgreSQL qualification for the scheduling repository.
//
// store_test.go uses a fake driver, which can prove transaction *shape* -- one
// transaction, rollback on failure, nesting refused, the ledger row locked
// first -- and nothing about SQL. Every property below needs a real server: the
// CHECK constraints that tie each projected column to the stored document, the
// aggregate that rebuilds the capacity ledger, FOR UPDATE serialization between
// concurrent schedulers, and the serializable isolation the store asks for.
//
// Opt-in through MINDCLADE_TEST_POSTGRES_DSN, and each test isolates into its
// own schema so a shared server cannot make two runs interfere.
package schedulingpostgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/lib/pq"

	"go.mindclade.dev/control/orchestration"
	"go.mindclade.dev/control/scheduling"
	auditpostgres "go.mindclade.dev/libs/go/audit/postgres"
	"go.mindclade.dev/libs/go/auth"
	"go.mindclade.dev/libs/go/clock"
	outboxpostgres "go.mindclade.dev/libs/go/coordination/outbox/postgres"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
)

const livePostgresEnvironment = "MINDCLADE_TEST_POSTGRES_DSN"

var liveSchedulingSchemaSequence atomic.Uint64

type liveSchedulingStore struct {
	store            *Store
	db               *sql.DB
	auditTable       string
	outboxTable      string
	reservationTable string
	quotaTable       string
	weightTable      string
	ledgerTable      string
}

func newLiveSchedulingStore(t *testing.T) liveSchedulingStore {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv(livePostgresEnvironment))
	if dsn == "" {
		t.Skipf("%s is not set; live PostgreSQL qualification is opt-in", livePostgresEnvironment)
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(32)
	db.SetMaxIdleConns(16)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		t.Fatalf("connect to live PostgreSQL: %v", err)
	}

	schema := fmt.Sprintf("mc_sched_qual_%d_%d", os.Getpid(), liveSchedulingSchemaSequence.Add(1))
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, openErr := sql.Open("postgres", dsn)
		if openErr == nil {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
			_, _ = cleanup.ExecContext(cleanupCtx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
			cleanupCancel()
			_ = cleanup.Close()
		}
		_ = db.Close()
	})

	live := liveSchedulingStore{
		db:               db,
		auditTable:       schema + ".audit_events",
		outboxTable:      schema + ".outbox_messages",
		reservationTable: schema + ".scheduling_reservations",
		quotaTable:       schema + ".scheduling_quotas",
		weightTable:      schema + ".scheduling_weights",
		ledgerTable:      schema + ".scheduling_ledger",
	}
	auditDDL, err := auditpostgres.DDL(live.auditTable)
	if err != nil {
		t.Fatal(err)
	}
	outboxDDL, err := outboxpostgres.DDL(live.outboxTable)
	if err != nil {
		t.Fatal(err)
	}
	schedulingDDL, err := DDL(live.reservationTable, live.quotaTable, live.weightTable, live.ledgerTable)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range append([]string{auditDDL, outboxDDL}, schedulingDDL...) {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("apply scheduling DDL: %v", err)
		}
	}
	live.store = live.newStore(t)
	return live
}

// newStore builds another adapter over the same tables. Two of them are what a
// restart looks like from the schema's point of view, which is the whole point
// of this package: the reservations outlive the process that recorded them.
func (live liveSchedulingStore) newStore(t *testing.T) *Store {
	t.Helper()
	recorder, err := auditpostgres.New(live.db, auditpostgres.WithTable(live.auditTable))
	if err != nil {
		t.Fatal(err)
	}
	messages, err := outboxpostgres.New(live.db, live.outboxTable)
	if err != nil {
		t.Fatal(err)
	}
	store, err := New(live.db, recorder, messages,
		WithClock(clock.RealClock{}),
		WithTables(live.reservationTable, live.quotaTable, live.weightTable, live.ledgerTable))
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func liveID(t *testing.T, kind string) identifiers.ID {
	t.Helper()
	id, err := identifiers.NewID(identifiers.MustParseKind(kind))
	if err != nil {
		t.Fatalf("new %s id: %v", kind, err)
	}
	return id
}

func (live liveSchedulingStore) count(t *testing.T, table string) int {
	t.Helper()
	var total int
	if err := live.db.QueryRowContext(context.Background(),
		"SELECT count(*) FROM "+table).Scan(&total); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return total
}

func liveBatchDomain(t *testing.T) scheduling.CapacityDomain {
	t.Helper()
	return testDomain(t, scheduling.WorkloadClassBatchCPU)
}

// seed records the fleet configuration every capacity test needs: one active
// capacity domain and one weighted tenant. Every ClusterQueue ships with a zero
// nominal quota and stopPolicy Hold, so without this the domain is inactive and
// nothing is admissible at all.
func (live liveSchedulingStore) seed(t *testing.T, tenants ...string) {
	t.Helper()
	ctx := context.Background()
	if err := live.store.PutQuota(ctx, liveBatchDomain(t),
		cpuDemand(64_000, 256*gibibyte, tebibyte, 128)); err != nil {
		t.Fatalf("PutQuota: %v", err)
	}
	for _, tenant := range tenants {
		if err := live.store.PutWeight(ctx, tenant, 1); err != nil {
			t.Fatalf("PutWeight %s: %v", tenant, err)
		}
	}
}

// livePlacement decides one placement against a snapshot, exactly the way
// Service.Place does: read the fleet, decide against that value, and let the
// store re-check it inside the write.
func livePlacement(
	t *testing.T, snapshot scheduling.FleetSnapshot, tenant string,
	priority scheduling.PriorityClass, demand scheduling.Demand, at time.Time,
) scheduling.Placement {
	t.Helper()
	request := scheduling.PlacementRequest{
		Admission: scheduling.AdmissionRequest{
			WorkloadID:  liveID(t, "workload"),
			Tenant:      tenant,
			Workspace:   "research-team",
			StageKind:   orchestration.StagePreprocess,
			Pool:        scheduling.PoolFeaturizationCPU,
			Accelerator: scheduling.AcceleratorNone,
			Priority:    priority,
			Demand:      demand,
			Replicas:    1,
		},
		RunID:   liveID(t, "run").String(),
		StageID: liveID(t, "stage").String(),
		Attempt: 1,
	}
	placement, err := snapshot.Place(request, at)
	if err != nil {
		t.Fatalf("place: %v", err)
	}
	return placement
}

func liveCandidate(
	t *testing.T, snapshot scheduling.FleetSnapshot, tenant string,
	priority scheduling.PriorityClass, demand scheduling.Demand,
	at time.Time, ttl time.Duration, fence uint64,
) scheduling.Reservation {
	t.Helper()
	placement := livePlacement(t, snapshot, tenant, priority, demand, at)
	candidate, err := scheduling.NewReservation(liveID(t, "reservation"), placement, fence, ttl)
	if err != nil {
		t.Fatalf("new reservation: %v", err)
	}
	return candidate
}

// reserve is the whole Place-then-Reserve round trip against the live store.
func (live liveSchedulingStore) reserve(
	t *testing.T, tenant string, priority scheduling.PriorityClass,
	demand scheduling.Demand, at time.Time, ttl time.Duration, fence uint64,
) scheduling.Reservation {
	t.Helper()
	ctx := context.Background()
	snapshot, err := live.store.Snapshot(ctx, at)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	candidate := liveCandidate(t, snapshot, tenant, priority, demand, at, ttl, fence)
	reservation, replayed, err := live.store.Reserve(ctx, snapshot, candidate, at)
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if replayed {
		t.Fatal("a first reservation must not replay")
	}
	return reservation
}

func liveNow() time.Time { return time.Now().Round(0).UTC() }

// The whole point of the single-transaction rule: the domain row, the audit
// record, and the outbox message either all land or none do.
func TestLivePostgresReservationWriteIsAtomicWithAuditAndOutbox(t *testing.T) {
	t.Run("commit", func(t *testing.T) {
		live := newLiveSchedulingStore(t)
		live.seed(t, "research")
		before := live.count(t, live.outboxTable)
		live.reserve(t, "research", scheduling.PriorityBatch,
			cpuDemand(2_000, 4*gibibyte, 8*gibibyte, 1), liveNow(), time.Minute, 7)
		if got := live.count(t, live.reservationTable); got != 1 {
			t.Fatalf("reservations = %d, want 1", got)
		}
		if got := live.count(t, live.outboxTable) - before; got != 1 {
			t.Fatalf("outbox messages = %d, want 1 for one reservation", got)
		}
	})

	t.Run("outbox failure rolls back domain and audit writes", func(t *testing.T) {
		live := newLiveSchedulingStore(t)
		live.seed(t, "research")
		ctx := context.Background()
		at := liveNow()
		snapshot, err := live.store.Snapshot(ctx, at)
		if err != nil {
			t.Fatalf("Snapshot: %v", err)
		}
		candidate := liveCandidate(t, snapshot, "research", scheduling.PriorityBatch,
			cpuDemand(2_000, 4*gibibyte, 8*gibibyte, 1), at, time.Minute, 7)
		audits := live.count(t, live.auditTable)
		// NOT VALID: the seed already appended messages, and PostgreSQL
		// refuses to add a CHECK the existing rows violate. NOT VALID applies
		// it to new rows only, which is exactly the failure being simulated.
		if _, err := live.db.ExecContext(ctx,
			"ALTER TABLE "+live.outboxTable+
				" ADD CONSTRAINT reject_all_messages CHECK (false) NOT VALID"); err != nil {
			t.Fatalf("install outbox failure: %v", err)
		}
		if _, _, err := live.store.Reserve(ctx, snapshot, candidate, at); err == nil {
			t.Fatal("Reserve must fail when its outbox append is rejected")
		}
		if got := live.count(t, live.reservationTable); got != 0 {
			t.Fatalf("reservations = %d, want 0 after rollback", got)
		}
		if got := live.count(t, live.auditTable); got != audits {
			t.Fatalf("audit events moved by %d, want 0 after rollback", got-audits)
		}
	})
}

// A CHECK constraint that never fires is indistinguishable from one that is not
// there. This writes a row whose projection disagrees with its document and
// requires the server to refuse it -- and then, in a second schema, drops that
// same constraint and requires the identical write to succeed. The second half
// is what makes the first half evidence: without it, a passing assertion could
// mean the constraint fired or that the UPDATE was malformed.
func TestLivePostgresRejectsProjectionDriftFromTheDocument(t *testing.T) {
	drift := func(live liveSchedulingStore, reservation scheduling.Reservation) error {
		// The document still says "held"; the column would say "bound".
		_, err := live.db.ExecContext(context.Background(),
			"UPDATE "+live.reservationTable+" SET state='bound' WHERE reservation_id=$1",
			reservation.ID.String())
		return err
	}

	t.Run("the constraint refuses the drift", func(t *testing.T) {
		live := newLiveSchedulingStore(t)
		live.seed(t, "research")
		reservation := live.reserve(t, "research", scheduling.PriorityBatch,
			cpuDemand(2_000, 4*gibibyte, 8*gibibyte, 1), liveNow(), time.Minute, 7)
		err := drift(live, reservation)
		if err == nil {
			t.Fatal("a state column that disagrees with its document must be refused")
		}
		if !strings.Contains(strings.ToLower(err.Error()), "constraint") {
			t.Fatalf("expected a constraint violation, got: %v", err)
		}
	})

	// Falsifiability, executed rather than asserted. The constraint is located
	// by name from the catalog and dropped; the same write must then land.
	t.Run("without the constraint the drift lands", func(t *testing.T) {
		live := newLiveSchedulingStore(t)
		live.seed(t, "research")
		reservation := live.reserve(t, "research", scheduling.PriorityBatch,
			cpuDemand(2_000, 4*gibibyte, 8*gibibyte, 1), liveNow(), time.Minute, 7)
		name := live.constraintNameContaining(t, live.reservationTable, "'state'")
		if _, err := live.db.ExecContext(context.Background(),
			"ALTER TABLE "+live.reservationTable+" DROP CONSTRAINT "+name); err != nil {
			t.Fatalf("drop %s: %v", name, err)
		}
		if err := drift(live, reservation); err != nil {
			t.Fatalf("with the constraint dropped the drift must land, got: %v", err)
		}
	})
}

// constraintNameContaining finds the one CHECK on a table whose expression
// mentions a fragment. It reads pg_constraint rather than assuming a name,
// because the server generates these names.
func (live liveSchedulingStore) constraintNameContaining(t *testing.T, qualified, fragment string) string {
	t.Helper()
	schema, table, found := strings.Cut(qualified, ".")
	if !found {
		t.Fatalf("table %q is not schema-qualified", qualified)
	}
	rows, err := live.db.QueryContext(context.Background(), `
SELECT con.conname
FROM pg_constraint AS con
JOIN pg_class AS cls ON cls.oid = con.conrelid
JOIN pg_namespace AS nsp ON nsp.oid = cls.relnamespace
WHERE nsp.nspname = $1 AND cls.relname = $2 AND con.contype = 'c'
  AND pg_get_constraintdef(con.oid) LIKE '%' || $3 || '%'`, schema, table, fragment)
	if err != nil {
		t.Fatalf("read constraints: %v", err)
	}
	defer func() { _ = rows.Close() }()
	names := make([]string, 0, 2)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan constraint: %v", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read constraints: %v", err)
	}
	if len(names) != 1 {
		t.Fatalf("expected exactly one CHECK mentioning %s, found %v", fragment, names)
	}
	return names[0]
}

// Divergence three, against a real ledger. Two schedulers that decided against
// the same snapshot cannot both commit: the second finds the fleet has moved
// and is refused inside the write, not warned about afterwards.
func TestLivePostgresConcurrentReserveAgainstOneSnapshotAdmitsOneWinner(t *testing.T) {
	live := newLiveSchedulingStore(t)
	live.seed(t, "research")
	ctx := context.Background()
	at := liveNow()
	snapshot, err := live.store.Snapshot(ctx, at)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	const racers = 8
	candidates := make([]scheduling.Reservation, 0, racers)
	for range racers {
		candidates = append(candidates, liveCandidate(t, snapshot, "research", scheduling.PriorityBatch,
			cpuDemand(2_000, 4*gibibyte, 8*gibibyte, 1), at, time.Minute, 7))
	}

	type outcome struct{ err error }
	results := make(chan outcome, racers)
	start := make(chan struct{})
	var ready, group sync.WaitGroup
	ready.Add(racers)
	group.Add(racers)
	for index := range racers {
		go func() {
			defer group.Done()
			ready.Done()
			<-start
			_, _, err := live.store.Reserve(ctx, snapshot, candidates[index], at)
			results <- outcome{err: err}
		}()
	}
	ready.Wait()
	close(start)
	group.Wait()
	close(results)

	admitted, stale := 0, 0
	for result := range results {
		switch {
		case result.err == nil:
			admitted++
		case faults.IsReason(result.err, "fleet_snapshot_stale"):
			stale++
		default:
			t.Errorf("unexpected racing outcome: %v", result.err)
		}
	}
	if admitted != 1 {
		t.Errorf("admitted = %d, want exactly 1 winner among %d racers", admitted, racers)
	}
	if stale != racers-1 {
		t.Errorf("stale = %d, want %d losers refused for a moved fleet", stale, racers-1)
	}
	if got := live.count(t, live.reservationTable); got != 1 {
		t.Fatalf("reservations = %d, want 1; the ledger was over-committed", got)
	}
}

// The staleness check has to be a check, not a constant refusal. A snapshot
// taken after the fleet moved admits; the one taken before it does not.
func TestLivePostgresFingerprintStalenessIsDecidedInsideTheWrite(t *testing.T) {
	live := newLiveSchedulingStore(t)
	live.seed(t, "research")
	ctx := context.Background()
	at := liveNow()
	before, err := live.store.Snapshot(ctx, at)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	// A new weighted tenant adds a ShareClaim at zero usage, which changes the
	// fair-share view and therefore the fingerprint -- the claim-set rule
	// stated as an observable consequence.
	if err := live.store.PutWeight(ctx, "platform", 1); err != nil {
		t.Fatalf("PutWeight: %v", err)
	}
	stale := liveCandidate(t, before, "research", scheduling.PriorityBatch,
		cpuDemand(2_000, 4*gibibyte, 8*gibibyte, 1), at, time.Minute, 7)
	_, _, err = live.store.Reserve(ctx, before, stale, at)
	if err == nil {
		t.Fatal("a decision taken against a moved fleet must be refused")
	}
	if !faults.IsReason(err, "fleet_snapshot_stale") {
		t.Fatalf("reason = %s, want fleet_snapshot_stale", faults.ReasonOf(err))
	}

	after, err := live.store.Snapshot(ctx, at)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	fresh := liveCandidate(t, after, "research", scheduling.PriorityBatch,
		cpuDemand(2_000, 4*gibibyte, 8*gibibyte, 1), at, time.Minute, 7)
	if _, _, err := live.store.Reserve(ctx, after, fresh, at); err != nil {
		t.Fatalf("a decision taken against the current fleet must be admitted: %v", err)
	}
}

// Divergence three's exact claim-set rule, pinned against the adapter it has to
// agree with. The same fleet driven through both stores must fingerprint
// identically, or a decision taken against one cannot be committed to the
// other -- which is what a factory swapping adapters would do.
//
// The fixture is chosen to hit both halves of the rule: "platform" has a weight
// and no usage, so it must appear as a claim with an empty Used vector, and
// "unweighted" has usage and no weight, so it must be absent from Claims while
// still counted in Reserved.
func TestLivePostgresSnapshotFingerprintMatchesTheMemoryAdapter(t *testing.T) {
	live := newLiveSchedulingStore(t)
	memory := scheduling.NewMemoryRepository(0)
	ctx := context.Background()
	domain := liveBatchDomain(t)
	nominal := cpuDemand(64_000, 256*gibibyte, tebibyte, 128)
	demand := cpuDemand(2_000, 4*gibibyte, 8*gibibyte, 1)
	at := liveNow()

	if err := live.store.PutQuota(ctx, domain, nominal); err != nil {
		t.Fatalf("live PutQuota: %v", err)
	}
	if err := memory.PutQuota(ctx, domain, nominal); err != nil {
		t.Fatalf("memory PutQuota: %v", err)
	}

	// The unweighted holder is admitted first, while no tenant has a weight at
	// all. That ordering is forced by the domain, not by convenience: once any
	// tenant is weighted, a tenant with no weight has a zero entitlement and a
	// weighted peer below its share, so fair share refuses it. It is still a
	// state the fleet reaches -- a weight recorded after work was admitted --
	// and it is the half of the claim-set rule that is easiest to get wrong.
	reserveBoth := func(tenant string) {
		t.Helper()
		memorySnapshot, err := memory.Snapshot(ctx, at)
		if err != nil {
			t.Fatalf("memory Snapshot: %v", err)
		}
		liveSnapshot, err := live.store.Snapshot(ctx, at)
		if err != nil {
			t.Fatalf("live Snapshot: %v", err)
		}
		if !liveSnapshot.Fingerprint().Equal(memorySnapshot.Fingerprint()) {
			t.Fatalf("fleets fingerprint differently before reserving %s:\nlive   %+v\nmemory %+v",
				tenant, liveSnapshot, memorySnapshot)
		}
		candidate := liveCandidate(t, memorySnapshot, tenant, scheduling.PriorityBatch,
			demand, at, time.Minute, 7)
		if _, _, err := memory.Reserve(ctx, memorySnapshot, candidate, at); err != nil {
			t.Fatalf("memory Reserve %s: %v", tenant, err)
		}
		if _, _, err := live.store.Reserve(ctx, liveSnapshot, candidate, at); err != nil {
			t.Fatalf("live Reserve %s: %v", tenant, err)
		}
	}
	reserveBoth("unweighted")

	for _, tenant := range []string{"research", "platform"} {
		if err := live.store.PutWeight(ctx, tenant, 1); err != nil {
			t.Fatalf("live PutWeight: %v", err)
		}
		if err := memory.PutWeight(ctx, tenant, 1); err != nil {
			t.Fatalf("memory PutWeight: %v", err)
		}
	}
	reserveBoth("research")

	liveSnapshot, err := live.store.Snapshot(ctx, at)
	if err != nil {
		t.Fatalf("live Snapshot: %v", err)
	}
	memorySnapshot, err := memory.Snapshot(ctx, at)
	if err != nil {
		t.Fatalf("memory Snapshot: %v", err)
	}
	if !liveSnapshot.Fingerprint().Equal(memorySnapshot.Fingerprint()) {
		t.Fatalf("occupied fleets fingerprint differently:\nlive   %+v\nmemory %+v",
			liveSnapshot, memorySnapshot)
	}

	// The rule, spelled out, so a fingerprint that matched for the wrong reason
	// still fails here.
	share, err := liveSnapshot.FairShare(domain)
	if err != nil {
		t.Fatalf("FairShare: %v", err)
	}
	if len(share.Claims) != 2 {
		t.Fatalf("claims = %d, want one per weighted tenant", len(share.Claims))
	}
	for _, claim := range share.Claims {
		if claim.Tenant == "unweighted" {
			t.Fatal("a tenant with usage and no weight must not appear as a claim")
		}
		if claim.Tenant == "platform" && !claim.Used.IsZero() {
			t.Fatal("a weighted tenant with no usage must appear with an empty Used vector")
		}
		if claim.Tenant == "research" && claim.Used[scheduling.ResourceCPU] != 2_000 {
			t.Fatalf("research used cpu = %d, want 2000", claim.Used[scheduling.ResourceCPU])
		}
	}
	allocatable, err := liveSnapshot.Allocatable(domain)
	if err != nil {
		t.Fatalf("Allocatable: %v", err)
	}
	if allocatable.Reserved[scheduling.ResourceCPU] != 4_000 {
		t.Fatalf("reserved cpu = %d, want 4000; an unweighted tenant still occupies capacity",
			allocatable.Reserved[scheduling.ResourceCPU])
	}
}

// Divergence one, against a real ledger. A hold past its deadline is re-sealed
// by the next ledger read and its capacity comes back, without anyone calling
// Expire.
func TestLivePostgresExpirySweepReturnsCapacity(t *testing.T) {
	live := newLiveSchedulingStore(t)
	live.seed(t, "research")
	ctx := context.Background()
	at := liveNow()
	reservation := live.reserve(t, "research", scheduling.PriorityBatch,
		cpuDemand(2_000, 4*gibibyte, 8*gibibyte, 1), at, scheduling.MinimumReservationTTL, 7)

	occupied, err := live.store.Snapshot(ctx, at)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	held, err := occupied.Allocatable(liveBatchDomain(t))
	if err != nil {
		t.Fatalf("Allocatable: %v", err)
	}
	if held.Reserved[scheduling.ResourceCPU] != 2_000 {
		t.Fatalf("reserved cpu = %d, want 2000 while the hold is live", held.Reserved[scheduling.ResourceCPU])
	}
	before := live.count(t, live.outboxTable)

	// The deadline is supplied, not waited for: every method takes its
	// transaction time explicitly, which is what makes expiry replayable.
	after := at.Add(scheduling.MinimumReservationTTL)
	swept, err := live.store.Snapshot(ctx, after)
	if err != nil {
		t.Fatalf("Snapshot after expiry: %v", err)
	}
	released, err := swept.Allocatable(liveBatchDomain(t))
	if err != nil {
		t.Fatalf("Allocatable: %v", err)
	}
	if !released.Reserved.IsZero() {
		t.Fatalf("reserved = %v, want empty; an expired hold is not occupied capacity", released.Reserved)
	}
	stored, err := live.store.Get(ctx, reservation.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if stored.State != scheduling.ReservationExpired {
		t.Fatalf("state = %s, want expired", stored.State)
	}
	// The sweep is a durable transition and is published like any other. A
	// silent expiry would leave every consumer holding a phantom reservation.
	if got := live.count(t, live.outboxTable) - before; got != 1 {
		t.Fatalf("outbox messages = %d, want 1 for the swept expiry", got)
	}
	// The expiry reuses the reservation's own fence rather than requiring a
	// live one, so an unattended control plane can still reclaim capacity.
	if stored.LeaseFence != reservation.LeaseFence {
		t.Fatalf("fence = %d, want the reservation's own %d", stored.LeaseFence, reservation.LeaseFence)
	}
	if _, err := live.store.Held(ctx, liveBatchDomain(t), after); err != nil {
		t.Fatalf("Held: %v", err)
	}
}

// Important 1 against a real audit table: a swept expiry is attributed to the
// system, not to whoever happened to call Snapshot.
//
// The fake-driver suite pins the actor the store hands to the recorder; this
// pins what actually lands in the audit table's actor_kind and actor_subject
// columns, which is what an operator reading the log would see. Both halves are
// exercised in one schema so the test cannot pass by attributing everything to
// the system: the caller-authored transition in the same run must still carry
// the caller.
func TestLivePostgresSweptExpiriesAreAuditedAsTheSystemActor(t *testing.T) {
	live := newLiveSchedulingStore(t)
	live.seed(t, "research")
	principal, err := auth.NewPrincipal(auth.PrincipalKindUser, "operator", auth.WithIssuer("mindclade"))
	if err != nil {
		t.Fatalf("new principal: %v", err)
	}
	ctx, err := auth.WithPrincipal(context.Background(), principal)
	if err != nil {
		t.Fatalf("context with principal: %v", err)
	}

	at := liveNow()
	lapsing := live.reserve(t, "research", scheduling.PriorityBatch,
		cpuDemand(2_000, 4*gibibyte, 8*gibibyte, 1), at, scheduling.MinimumReservationTTL, 7)
	surviving := live.reserve(t, "research", scheduling.PriorityBatch,
		cpuDemand(2_000, 4*gibibyte, 8*gibibyte, 1), at, time.Hour, 7)

	// An operator reads the fleet after the first hold has lapsed. The read
	// sweeps it; the operator did not cause it.
	after := at.Add(scheduling.MinimumReservationTTL)
	if _, err := live.store.Snapshot(ctx, after); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	kind, subject := live.auditActor(t, lapsing.ID)
	if kind != string(auth.PrincipalKindSystem) || subject != "scheduling-service" {
		t.Fatalf("swept expiry audited as %s/%s, want system/scheduling-service", kind, subject)
	}
	if subject == principal.Subject() {
		t.Fatalf("the calling operator %q was recorded as the author of a deadline-authored expiry", subject)
	}

	// The same operator binds the other reservation. That one they did cause,
	// and forcing the system actor everywhere would erase them from the log.
	if _, _, err := live.store.Bind(ctx, surviving.ID, surviving.Version,
		scheduling.TopologyAssignment{}, 7, after); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	kind, subject = live.auditActor(t, surviving.ID)
	if kind != string(auth.PrincipalKindUser) || subject != principal.Subject() {
		t.Fatalf("caller-authored bind audited as %s/%s, want user/%s", kind, subject, principal.Subject())
	}
}

// auditActor reads the actor of the most recent audit event naming one
// reservation as its target.
func (live liveSchedulingStore) auditActor(t *testing.T, id identifiers.ID) (string, string) {
	t.Helper()
	var kind, subject string
	err := live.db.QueryRowContext(context.Background(),
		"SELECT actor_kind, actor_subject FROM "+live.auditTable+
			" WHERE target_type=$1 AND target_id=$2 ORDER BY occurred_at DESC, inserted_at DESC LIMIT 1",
		ReservationTargetType, id.String()).Scan(&kind, &subject)
	if err != nil {
		t.Fatalf("read audit actor for %s: %v", id, err)
	}
	return kind, subject
}

// The bounded sweep drains a backlog across successive mutations rather than
// clearing it in one long transaction, and each pass releases the ledger row
// that every other mutation must acquire first.
//
// MaximumExpirySweep + 1 lapsed holds means the first pass cannot finish the
// job. Nothing wedges: the store keeps answering, and the next read completes
// the drain. This is the property that makes a small batch safe.
func TestLivePostgresExpirySweepDrainsABacklogAcrossMutations(t *testing.T) {
	live := newLiveSchedulingStore(t)
	live.seed(t, "research")
	ctx := context.Background()
	at := liveNow()

	total := MaximumExpirySweep + 1
	for range total {
		live.reserve(t, "research", scheduling.PriorityBatch,
			cpuDemand(1, 1, 1, 1), at, scheduling.MinimumReservationTTL, 7)
	}
	after := at.Add(scheduling.MinimumReservationTTL)

	// One pass sweeps at most MaximumExpirySweep, so the ledger is still
	// carrying the remainder -- over-reporting occupied capacity, which refuses
	// an admission rather than over-committing one.
	if _, err := live.store.Snapshot(ctx, after); err != nil {
		t.Fatalf("first Snapshot: %v", err)
	}
	remaining := live.countHeld(t)
	if remaining != total-MaximumExpirySweep {
		t.Fatalf("held after one pass = %d, want %d; the sweep is not bounded at %d",
			remaining, total-MaximumExpirySweep, MaximumExpirySweep)
	}

	// The next mutation finishes it. Every mutation sweeps, so the backlog
	// drains at the rate the control plane is doing anything at all.
	snapshot, err := live.store.Snapshot(ctx, after)
	if err != nil {
		t.Fatalf("second Snapshot: %v", err)
	}
	if got := live.countHeld(t); got != 0 {
		t.Fatalf("held after two passes = %d, want 0", got)
	}
	allocatable, err := snapshot.Allocatable(liveBatchDomain(t))
	if err != nil {
		t.Fatalf("Allocatable: %v", err)
	}
	if !allocatable.Reserved.IsZero() {
		t.Fatalf("reserved = %v, want empty once the backlog drained", allocatable.Reserved)
	}
}

func (live liveSchedulingStore) countHeld(t *testing.T) int {
	t.Helper()
	var total int
	if err := live.db.QueryRowContext(context.Background(),
		"SELECT count(*) FROM "+live.reservationTable+" WHERE state='held'").Scan(&total); err != nil {
		t.Fatalf("count held: %v", err)
	}
	return total
}

// Preemption is all-or-nothing and multi-victim. Both victims move in one
// transaction, both release their capacity, and re-applying the same plan
// replays instead of evicting anything twice.
func TestLivePostgresMultiVictimPreemptionIsOneTransaction(t *testing.T) {
	live := newLiveSchedulingStore(t)
	live.seed(t, "research")
	ctx := context.Background()
	at := liveNow()
	domain := liveBatchDomain(t)
	demand := cpuDemand(2_000, 4*gibibyte, 8*gibibyte, 1)

	first := live.reserve(t, "research", scheduling.PriorityBatch, demand, at, time.Minute, 7)
	second := live.reserve(t, "research", scheduling.PriorityBatch, demand, at, time.Minute, 7)

	snapshot, err := live.store.Snapshot(ctx, at)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	share, err := snapshot.FairShare(domain)
	if err != nil {
		t.Fatalf("FairShare: %v", err)
	}
	held, err := live.store.Held(ctx, domain, at)
	if err != nil {
		t.Fatalf("Held: %v", err)
	}
	if len(held) != 2 {
		t.Fatalf("held = %d, want 2", len(held))
	}
	shortfall, err := demand.Scale(2)
	if err != nil {
		t.Fatalf("scale: %v", err)
	}
	plan, err := scheduling.SelectVictims(scheduling.PreemptionRequest{
		Candidate: liveID(t, "reservation"),
		Domain:    domain,
		Tenant:    "platform",
		Priority:  scheduling.PriorityPlatformCritical,
		Shortfall: shortfall,
	}, held, share, at)
	if err != nil {
		t.Fatalf("SelectVictims: %v", err)
	}
	if len(plan.Victims) != 2 {
		t.Fatalf("victims = %d, want 2", len(plan.Victims))
	}

	before := live.count(t, live.outboxTable)
	evicted, replayed, err := live.store.Preempt(ctx, plan, 8, at)
	if err != nil {
		t.Fatalf("Preempt: %v", err)
	}
	if replayed {
		t.Fatal("a first preemption must not replay")
	}
	if len(evicted) != 2 {
		t.Fatalf("evicted = %d, want 2", len(evicted))
	}
	if got := live.count(t, live.outboxTable) - before; got != 2 {
		t.Fatalf("outbox messages = %d, want one per victim", got)
	}
	for _, id := range []identifiers.ID{first.ID, second.ID} {
		stored, err := live.store.Get(ctx, id)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if stored.State != scheduling.ReservationPreempted {
			t.Fatalf("state = %s, want preempted", stored.State)
		}
		if stored.Preemptor != plan.Candidate {
			t.Fatal("a preempted reservation must name its preemptor")
		}
	}
	released, err := live.store.Snapshot(ctx, at)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	allocatable, err := released.Allocatable(domain)
	if err != nil {
		t.Fatalf("Allocatable: %v", err)
	}
	if !allocatable.Reserved.IsZero() {
		t.Fatalf("reserved = %v, want empty after both victims were evicted", allocatable.Reserved)
	}

	// A crash between the write and the ack re-drives the same plan. It must
	// replay rather than fail or evict a second time.
	again, replayed, err := live.store.Preempt(ctx, plan, 8, at)
	if err != nil {
		t.Fatalf("replayed Preempt: %v", err)
	}
	if !replayed || len(again) != 2 {
		t.Fatalf("replayed = %v, victims = %d; a re-driven plan must replay", replayed, len(again))
	}
	if got := live.count(t, live.outboxTable) - before; got != 2 {
		t.Fatalf("outbox messages = %d, want no new message for a replay", got)
	}
}

// Two workers racing one binding must produce one transition, not two.
// FOR UPDATE is what serializes them, and only a real server has it.
func TestLivePostgresConcurrentBindsProduceOneWinner(t *testing.T) {
	live := newLiveSchedulingStore(t)
	live.seed(t, "research")
	ctx := context.Background()
	at := liveNow()
	reservation := live.reserve(t, "research", scheduling.PriorityBatch,
		cpuDemand(2_000, 4*gibibyte, 8*gibibyte, 1), at, time.Minute, 7)

	const racers = 8
	type outcome struct {
		replayed bool
		err      error
	}
	results := make(chan outcome, racers)
	start := make(chan struct{})
	var ready, group sync.WaitGroup
	ready.Add(racers)
	group.Add(racers)
	for range racers {
		go func() {
			defer group.Done()
			ready.Done()
			<-start
			_, replayed, err := live.store.Bind(ctx, reservation.ID, reservation.Version,
				scheduling.TopologyAssignment{}, 7, at.Add(time.Second))
			results <- outcome{replayed: replayed, err: err}
		}()
	}
	ready.Wait()
	close(start)
	group.Wait()
	close(results)

	applied, replayed := 0, 0
	for result := range results {
		if result.err != nil {
			t.Errorf("racing bind returned an unexpected error: %v", result.err)
			continue
		}
		if result.replayed {
			replayed++
		} else {
			applied++
		}
	}
	if applied != 1 {
		t.Errorf("applied = %d, want exactly 1 winner among %d racers", applied, racers)
	}
	if replayed != racers-1 {
		t.Errorf("replayed = %d, want %d successful losing replays", replayed, racers-1)
	}
	stored, err := live.store.Get(ctx, reservation.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if stored.State != scheduling.ReservationBound || stored.Sequence != 1 {
		t.Fatalf("state = %s sequence = %d, want bound at sequence 1", stored.State, stored.Sequence)
	}
}

// A reservation outlives the process that recorded it. This is the property the
// package exists for, and nothing above proves it: every other test reads back
// through the same adapter instance that wrote.
func TestLivePostgresReservationsSurviveANewStore(t *testing.T) {
	live := newLiveSchedulingStore(t)
	live.seed(t, "research")
	ctx := context.Background()
	at := liveNow()
	reservation := live.reserve(t, "research", scheduling.PriorityBatch,
		cpuDemand(2_000, 4*gibibyte, 8*gibibyte, 1), at, time.Minute, 7)

	restarted := live.newStore(t)
	stored, err := restarted.Get(ctx, reservation.ID)
	if err != nil {
		t.Fatalf("Get after restart: %v", err)
	}
	if stored.Version.String() != reservation.Version.String() {
		t.Fatalf("version = %s, want %s", stored.Version, reservation.Version)
	}
	if !stored.Placement.Digest.Equal(reservation.Placement.Digest) {
		t.Fatal("the placement digest did not survive the round trip")
	}
	snapshot, err := restarted.Snapshot(ctx, at)
	if err != nil {
		t.Fatalf("Snapshot after restart: %v", err)
	}
	allocatable, err := snapshot.Allocatable(liveBatchDomain(t))
	if err != nil {
		t.Fatalf("Allocatable: %v", err)
	}
	if allocatable.Reserved[scheduling.ResourceCPU] != 2_000 {
		t.Fatalf("reserved cpu = %d, want 2000; the ledger did not survive the restart",
			allocatable.Reserved[scheduling.ResourceCPU])
	}
	// The fence survives too, so a former leader cannot resume writing after a
	// restart handed leadership on.
	_, _, err = restarted.Complete(ctx, reservation.ID, reservation.Version, 6, at.Add(time.Second))
	if err == nil {
		t.Fatal("a write carrying an older fence must be refused after a restart")
	}
	if !faults.IsReason(err, "lease_fence_stale") {
		t.Fatalf("reason = %s, want lease_fence_stale", faults.ReasonOf(err))
	}
}

// A quota may not be reduced below what is already held: the ledger would be
// permanently over-reserved with no transition able to repair it.
func TestLivePostgresQuotaCannotBeReducedBelowHeldCapacity(t *testing.T) {
	live := newLiveSchedulingStore(t)
	live.seed(t, "research")
	ctx := context.Background()
	live.reserve(t, "research", scheduling.PriorityBatch,
		cpuDemand(2_000, 4*gibibyte, 8*gibibyte, 1), liveNow(), time.Minute, 7)

	err := live.store.PutQuota(ctx, liveBatchDomain(t), cpuDemand(1_000, gibibyte, gibibyte, 1))
	if err == nil {
		t.Fatal("a quota below the held total must be refused")
	}
	if !faults.IsReason(err, "quota_below_reserved") {
		t.Fatalf("reason = %s, want quota_below_reserved", faults.ReasonOf(err))
	}
	// The refusal must not have moved anything: raising the quota still works.
	if err := live.store.PutQuota(ctx, liveBatchDomain(t),
		cpuDemand(128_000, 512*gibibyte, tebibyte, 256)); err != nil {
		t.Fatalf("raising the quota must be permitted: %v", err)
	}
}

// A retried placement is the same reservation. The placement key is the
// idempotency identity, so the second call returns the first reservation rather
// than charging the ledger twice.
func TestLivePostgresRetriedPlacementReplays(t *testing.T) {
	live := newLiveSchedulingStore(t)
	live.seed(t, "research")
	ctx := context.Background()
	at := liveNow()
	snapshot, err := live.store.Snapshot(ctx, at)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	candidate := liveCandidate(t, snapshot, "research", scheduling.PriorityBatch,
		cpuDemand(2_000, 4*gibibyte, 8*gibibyte, 1), at, time.Minute, 7)
	first, replayed, err := live.store.Reserve(ctx, snapshot, candidate, at)
	if err != nil || replayed {
		t.Fatalf("Reserve: err=%v replayed=%v", err, replayed)
	}

	// A retry mints a fresh reservation id and a fresh decision, but the run,
	// stage, and attempt are the same -- which is what PlacementKey keys on.
	retried, err := live.store.Snapshot(ctx, at)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	request := scheduling.PlacementRequest{
		Admission: scheduling.AdmissionRequest{
			WorkloadID:  candidate.Placement.WorkloadID,
			Tenant:      candidate.Placement.Tenant,
			Workspace:   candidate.Placement.Workspace,
			StageKind:   orchestration.StagePreprocess,
			Pool:        scheduling.PoolFeaturizationCPU,
			Accelerator: scheduling.AcceleratorNone,
			Priority:    candidate.Placement.Priority,
			Demand:      candidate.Placement.PerReplica.Clone(),
			Replicas:    candidate.Placement.Replicas,
		},
		RunID:   candidate.Placement.RunID,
		StageID: candidate.Placement.StageID,
		Attempt: candidate.Placement.Attempt,
	}
	placement, err := retried.Place(request, at)
	if err != nil {
		t.Fatalf("place: %v", err)
	}
	second, err := scheduling.NewReservation(liveID(t, "reservation"), placement, 7, time.Minute)
	if err != nil {
		t.Fatalf("new reservation: %v", err)
	}
	stored, replayed, err := live.store.Reserve(ctx, retried, second, at)
	if err != nil {
		t.Fatalf("retried Reserve: %v", err)
	}
	if !replayed {
		t.Fatal("a retried placement must replay")
	}
	if stored.ID != first.ID {
		t.Fatalf("replay returned %s, want the original %s", stored.ID, first.ID)
	}
	if got := live.count(t, live.reservationTable); got != 1 {
		t.Fatalf("reservations = %d, want 1; a retry must not charge the ledger twice", got)
	}
}

// The zero instant is stored, not omitted. time.Time is a struct, so a JSON
// `omitempty` tag does not drop it, and the bound_at / finalized_at columns are
// NOT NULL as a result. Year one has to survive the round trip through
// timestamptz or every held reservation would fail its own CHECK.
func TestLivePostgresStoresTheZeroInstantForUnsetLifecycleTimes(t *testing.T) {
	live := newLiveSchedulingStore(t)
	live.seed(t, "research")
	reservation := live.reserve(t, "research", scheduling.PriorityBatch,
		cpuDemand(2_000, 4*gibibyte, 8*gibibyte, 1), liveNow(), time.Minute, 7)
	var boundAt, finalizedAt time.Time
	err := live.db.QueryRowContext(context.Background(),
		"SELECT bound_at, finalized_at FROM "+live.reservationTable+" WHERE reservation_id=$1",
		reservation.ID.String()).Scan(&boundAt, &finalizedAt)
	if err != nil {
		t.Fatalf("read lifecycle times: %v", err)
	}
	if !boundAt.IsZero() || !finalizedAt.IsZero() {
		t.Fatalf("bound_at=%s finalized_at=%s, want the zero instant", boundAt, finalizedAt)
	}
}
