// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package scheduler

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"go.mindclade.dev/control/orchestration"
	"go.mindclade.dev/control/scheduling"
	"go.mindclade.dev/libs/go/coordination/leadership"
	"go.mindclade.dev/libs/go/coordination/workqueue"
	workqueuememory "go.mindclade.dev/libs/go/coordination/workqueue/memory"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/requestmeta"
	"go.mindclade.dev/libs/go/servicekit"
	"go.mindclade.dev/libs/go/storage/lease"
)

type contextKey struct{}

// recordingQueue is the memory work-queue store with the appends it was handed
// captured, plus the context each one arrived on. The context is what proves
// the producer appended on the caller's transaction rather than on one of its
// own: workqueue/postgres reads its executor out of the context, so a producer
// that substituted a different one would be writing on a different connection.
type recordingQueue struct {
	*workqueuememory.Store
	items    []workqueue.Item
	contexts []context.Context
	failure  error
}

func newRecordingQueue() *recordingQueue {
	return &recordingQueue{Store: workqueuememory.New()}
}

func (queue *recordingQueue) Enqueue(ctx context.Context, item workqueue.Item) error {
	queue.contexts = append(queue.contexts, ctx)
	if queue.failure != nil {
		return queue.failure
	}
	if err := queue.Store.Enqueue(ctx, item); err != nil {
		return err
	}
	queue.items = append(queue.items, item)
	return nil
}

func testID(t *testing.T, kind string) identifiers.ID {
	t.Helper()
	id, err := identifiers.NewID(identifiers.MustParseKind(kind))
	if err != nil {
		t.Fatalf("mint a %s identifier: %v", kind, err)
	}
	return id
}

// admissionFor is the facts a promoted stage would resolve to in a deployment
// that had a facts source wired. It is a batch CPU featurization stage, which
// is the cheapest shape that still exercises pool resolution, demand scaling
// and the priority projection.
func admissionFor(t *testing.T) scheduling.AdmissionRequest {
	t.Helper()
	return scheduling.AdmissionRequest{
		WorkloadID:  testID(t, "workload"),
		Tenant:      "alpha-lab",
		Workspace:   "placement",
		StageKind:   orchestration.StagePreprocess,
		Pool:        scheduling.PoolFeaturizationCPU,
		Accelerator: scheduling.AcceleratorNone,
		Priority:    scheduling.PriorityBatch,
		Demand: scheduling.Demand{
			scheduling.ResourceCPU:              2_000,
			scheduling.ResourceMemory:           4 << 30,
			scheduling.ResourceEphemeralStorage: 8 << 30,
			scheduling.ResourcePods:             1,
		},
		Replicas: 1,
	}
}

func workItemFor(t *testing.T) orchestration.WorkItem {
	t.Helper()
	return orchestration.WorkItem{
		RunID:   testID(t, "run").String(),
		JobID:   testID(t, "job").String(),
		StageID: testID(t, "stage").String(),
		Attempt: 1,
	}
}

// fleetWithCapacity is a scheduling repository holding enough batch CPU quota
// for the admission above.
func fleetWithCapacity(t *testing.T) *scheduling.MemoryRepository {
	t.Helper()
	repository := scheduling.NewMemoryRepository(0)
	domain, err := scheduling.DomainFor(scheduling.WorkloadClassBatchCPU)
	if err != nil {
		t.Fatalf("batch-cpu capacity domain: %v", err)
	}
	err = repository.PutQuota(context.Background(), domain, scheduling.Demand{
		scheduling.ResourceCPU:              64_000,
		scheduling.ResourceMemory:           256 << 30,
		scheduling.ResourceEphemeralStorage: 1 << 40,
		scheduling.ResourcePods:             128,
	})
	if err != nil {
		t.Fatalf("PutQuota: %v", err)
	}
	return repository
}

// The seam this task exists to close, driven end to end: a promoted stage is
// translated by the producer, and the item that results is one the scheduler's
// own handler accepts and places.
//
// Nothing in the type system connects those two halves. The producer writes a
// []byte onto a string-named queue and the handler reads a []byte off it, so a
// producer that emitted the wrong shape would compile, enqueue, and be
// dead-lettered at the far end with the promotion already committed.
func TestPlacementProducerTranslatesIntoAnItemTheSchedulerPlaces(t *testing.T) {
	ctx := context.Background()
	queue := newRecordingQueue()
	admission := admissionFor(t)
	producer, err := newPlacementProducer(queue, PlacementFactsFunc(
		func(context.Context, orchestration.WorkItem) (scheduling.AdmissionRequest, error) {
			return admission, nil
		}))
	if err != nil {
		t.Fatalf("newPlacementProducer: %v", err)
	}
	work := workItemFor(t)
	if err := producer.EnqueueStage(ctx, work); err != nil {
		t.Fatalf("EnqueueStage: %v", err)
	}
	if len(queue.items) != 1 {
		t.Fatalf("appended %d items, want exactly one", len(queue.items))
	}
	item := queue.items[0]
	if item.Queue != scheduling.PlacementQueue {
		t.Fatalf("item queue = %q, want %q", item.Queue, scheduling.PlacementQueue)
	}
	if item.MaxAttempts != placementItemMaxAttempts {
		t.Fatalf("item max attempts = %d, want the bounded %d", item.MaxAttempts, placementItemMaxAttempts)
	}

	service := scheduling.Service{Repository: fleetWithCapacity(t), Fence: 7}
	result, err := service.Handle(ctx, item)
	if err != nil {
		t.Fatalf("the scheduler refused the item its own producer minted: %v", err)
	}
	var decision scheduling.Decision
	if err := json.Unmarshal(result.Payload, &decision); err != nil {
		t.Fatalf("decode placement decision: %v", err)
	}
	if decision.Reservation.Placement.RunID != work.RunID ||
		decision.Reservation.Placement.StageID != work.StageID ||
		decision.Reservation.Placement.Attempt != work.Attempt {
		t.Fatalf("placed %+v, want the promoted stage's coordinates %+v",
			decision.Reservation.Placement, work)
	}
	if decision.Reservation.Placement.Tenant != admission.Tenant {
		t.Fatalf("placement tenant = %q, want %q", decision.Reservation.Placement.Tenant, admission.Tenant)
	}
}

// The falsifiability half of the test above: the raw orchestration payload is
// NOT interchangeable with the translated one. Task 5's live test used a
// test-local queue name and the raw payload, so nothing before this asserted
// what the placement queue actually demands.
func TestARawWorkItemPayloadIsRefusedByThePlacementHandler(t *testing.T) {
	ctx := context.Background()
	payload, err := orchestration.EncodeWorkItem(workItemFor(t))
	if err != nil {
		t.Fatalf("EncodeWorkItem: %v", err)
	}
	item, err := workqueue.NewItem(
		scheduling.PlacementQueue, payload, 0, time.Time{}, placementItemMaxAttempts, requestmeta.Metadata{})
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	service := scheduling.Service{Repository: fleetWithCapacity(t), Fence: 7}
	_, err = service.Handle(ctx, item)
	if !faults.IsReason(err, "placement_command_invalid") {
		t.Fatalf("a raw work item on the placement queue = %s/%q (%v), want placement_command_invalid",
			faults.CodeOf(err), faults.ReasonOf(err), err)
	}
}

// The append must ride the caller's context. workqueue/postgres resolves its
// executor with transaction.FromContext, so a producer that built its own
// context -- or held its own *sql.DB -- would commit the item outside the
// promotion's transaction, and a crash in between would leave a promoted stage
// nothing places or a placement for a promotion that rolled back.
func TestPlacementProducerAppendsOnTheCallersContext(t *testing.T) {
	sentinel := context.WithValue(context.Background(), contextKey{}, "transaction")
	queue := newRecordingQueue()
	producer, err := newPlacementProducer(queue, PlacementFactsFunc(
		func(context.Context, orchestration.WorkItem) (scheduling.AdmissionRequest, error) {
			return admissionFor(t), nil
		}))
	if err != nil {
		t.Fatalf("newPlacementProducer: %v", err)
	}
	if err := producer.EnqueueStage(sentinel, workItemFor(t)); err != nil {
		t.Fatalf("EnqueueStage: %v", err)
	}
	if len(queue.contexts) != 1 {
		t.Fatalf("queue saw %d contexts, want one", len(queue.contexts))
	}
	if queue.contexts[0].Value(contextKey{}) != "transaction" {
		t.Fatal("the append did not arrive on the caller's context")
	}
}

// The facts source is also read on that context, for the same reason: an
// implementation resolving its executor from it reads the same uncommitted
// state the promotion is writing.
func TestPlacementFactsAreResolvedOnTheCallersContext(t *testing.T) {
	sentinel := context.WithValue(context.Background(), contextKey{}, "transaction")
	var seen context.Context
	producer, err := newPlacementProducer(newRecordingQueue(), PlacementFactsFunc(
		func(ctx context.Context, _ orchestration.WorkItem) (scheduling.AdmissionRequest, error) {
			seen = ctx
			return admissionFor(t), nil
		}))
	if err != nil {
		t.Fatalf("newPlacementProducer: %v", err)
	}
	if err := producer.EnqueueStage(sentinel, workItemFor(t)); err != nil {
		t.Fatalf("EnqueueStage: %v", err)
	}
	if seen == nil || seen.Value(contextKey{}) != "transaction" {
		t.Fatal("the facts source was not resolved on the caller's context")
	}
}

// A promotion whose facts do not describe a placeable workload must fail the
// promotion, not queue an item nothing can ever place. The stage rolls back to
// where a reconciler can retry it; a durably queued unplaceable item would not.
func TestPlacementProducerRefusesAnUnplaceableAdmission(t *testing.T) {
	queue := newRecordingQueue()
	incomplete := admissionFor(t)
	incomplete.Tenant = ""
	producer, err := newPlacementProducer(queue, PlacementFactsFunc(
		func(context.Context, orchestration.WorkItem) (scheduling.AdmissionRequest, error) {
			return incomplete, nil
		}))
	if err != nil {
		t.Fatalf("newPlacementProducer: %v", err)
	}
	if err := producer.EnqueueStage(context.Background(), workItemFor(t)); err == nil {
		t.Fatal("an admission with no tenant was accepted")
	}
	if len(queue.items) != 0 {
		t.Fatalf("appended %d items for a refused promotion, want none", len(queue.items))
	}
}

// The facts source's own failure travels out unchanged. The repository reads
// its code and retry policy to tell a serialization conflict that must be
// replayed from a payload that never can be, and restating it here would
// flatten that distinction.
func TestPlacementProducerReturnsTheFactsFailureUnchanged(t *testing.T) {
	failure := faults.New(faults.CodeAborted, "read conflicted",
		faults.WithReason("scheduling_serialization_retry"))
	producer, err := newPlacementProducer(newRecordingQueue(), PlacementFactsFunc(
		func(context.Context, orchestration.WorkItem) (scheduling.AdmissionRequest, error) {
			return scheduling.AdmissionRequest{}, failure
		}))
	if err != nil {
		t.Fatalf("newPlacementProducer: %v", err)
	}
	err = producer.EnqueueStage(context.Background(), workItemFor(t))
	if !faults.IsCode(err, faults.CodeAborted) || !faults.IsReason(err, "scheduling_serialization_retry") {
		t.Fatalf("producer error = %s/%q, want the facts failure verbatim", faults.CodeOf(err), faults.ReasonOf(err))
	}
}

func TestPlacementProducerRefusesAnInvalidWorkItem(t *testing.T) {
	queue := newRecordingQueue()
	producer, err := newPlacementProducer(queue, PlacementFactsFunc(
		func(context.Context, orchestration.WorkItem) (scheduling.AdmissionRequest, error) {
			return admissionFor(t), nil
		}))
	if err != nil {
		t.Fatalf("newPlacementProducer: %v", err)
	}
	work := workItemFor(t)
	work.Attempt = 0
	if err := producer.EnqueueStage(context.Background(), work); err == nil {
		t.Fatal("a work item with no attempt was accepted")
	}
	if len(queue.items) != 0 {
		t.Fatalf("appended %d items for an invalid work item, want none", len(queue.items))
	}
}

// nilFacts exists to be a typed nil. A (*nilFacts)(nil) is not == nil, so it is
// exactly the wiring mistake a plain guard would let through.
type nilFacts struct{}

func (*nilFacts) Admission(context.Context, orchestration.WorkItem) (scheduling.AdmissionRequest, error) {
	return scheduling.AdmissionRequest{}, nil
}

func TestNewPlacementProducerRequiresItsCollaborators(t *testing.T) {
	usable := PlacementFactsFunc(func(context.Context, orchestration.WorkItem) (scheduling.AdmissionRequest, error) {
		return scheduling.AdmissionRequest{}, nil
	})
	if _, err := newPlacementProducer(nil, usable); !faults.IsReason(err, "placement_queue_missing") {
		t.Fatalf("nil queue = %q, want placement_queue_missing", faults.ReasonOf(err))
	}
	if _, err := newPlacementProducer((*workqueuememory.Store)(nil), usable); !faults.IsReason(err, "placement_queue_missing") {
		t.Fatalf("typed nil queue = %q, want placement_queue_missing", faults.ReasonOf(err))
	}
	if _, err := newPlacementProducer(newRecordingQueue(), nil); !faults.IsReason(err, "placement_facts_missing") {
		t.Fatalf("nil facts = %q, want placement_facts_missing", faults.ReasonOf(err))
	}
	if _, err := newPlacementProducer(newRecordingQueue(), (*nilFacts)(nil)); !faults.IsReason(err, "placement_facts_missing") {
		t.Fatalf("typed nil facts = %q, want placement_facts_missing", faults.ReasonOf(err))
	}
}

// The single highest-risk detail in this wiring, asserted three ways.
//
// A handler that captured the fence once -- at construction, or from a bind
// callback fired when leadership was acquired -- compiles, type-checks, and
// passes every test that never renews or ends an epoch. It would then quote a
// dead epoch forever: the store refuses a write whose fence is below the
// highest it has accepted, which is how a deposed leader is stopped, so a live
// leader quoting a stale fence is refused exactly like a deposed one.
//
// Each leg below fails under that spelling: the first because a captured fence
// is whatever the second epoch happened to be, the second because it would be
// the first epoch's, and the third because a released view would still report a
// number this process no longer holds.
func TestFencedPlacementReadsTheFenceOnEveryCall(t *testing.T) {
	ctx := context.Background()
	service := scheduling.Service{Repository: fleetWithCapacity(t)}
	view := &leadership.SessionView{}
	handler := fencedPlacement(service, view)

	admission := admissionFor(t)
	place := func(t *testing.T) uint64 {
		t.Helper()
		queue := newRecordingQueue()
		producer, err := newPlacementProducer(queue, PlacementFactsFunc(
			func(context.Context, orchestration.WorkItem) (scheduling.AdmissionRequest, error) {
				admission.WorkloadID = testID(t, "workload")
				return admission, nil
			}))
		if err != nil {
			t.Fatalf("newPlacementProducer: %v", err)
		}
		if err := producer.EnqueueStage(ctx, workItemFor(t)); err != nil {
			t.Fatalf("EnqueueStage: %v", err)
		}
		result, err := handler(ctx, queue.items[0])
		if err != nil {
			t.Fatalf("handle a placement under leadership: %v", err)
		}
		var decision scheduling.Decision
		if err := json.Unmarshal(result.Payload, &decision); err != nil {
			t.Fatalf("decode placement decision: %v", err)
		}
		return decision.Reservation.LeaseFence
	}

	// The gate is what binds the view for the duration of one epoch, so the
	// handler has to be driven from inside a gated run.
	var fences []uint64
	gate, gated, err := leadership.GateComponentWithSession(servicekit.Component{
		Name: "placement-epoch",
		Run: func(context.Context) error {
			fences = append(fences, place(t))
			return nil
		},
	}, view)
	if err != nil {
		t.Fatalf("GateComponentWithSession: %v", err)
	}
	if gated.Run != nil {
		t.Fatal("a gated component kept an independent run loop")
	}
	for _, version := range []uint64{7, 9} {
		if err := gate(ctx, leadership.Session{Lease: lease.Lease{Version: version}}); err != nil {
			t.Fatalf("run the gated epoch %d: %v", version, err)
		}
	}
	if len(fences) != 2 || fences[0] != 7 || fences[1] != 9 {
		t.Fatalf("placements carried fences %v, want [7 9] -- one per epoch", fences)
	}

	// Outside an epoch the view reports nothing, and the domain refuses to
	// write rather than reserving fleet capacity it cannot prove authority for.
	queue := newRecordingQueue()
	producer, err := newPlacementProducer(queue, PlacementFactsFunc(
		func(context.Context, orchestration.WorkItem) (scheduling.AdmissionRequest, error) {
			return admissionFor(t), nil
		}))
	if err != nil {
		t.Fatalf("newPlacementProducer: %v", err)
	}
	if err := producer.EnqueueStage(ctx, workItemFor(t)); err != nil {
		t.Fatalf("EnqueueStage: %v", err)
	}
	if _, err := handler(ctx, queue.items[0]); !faults.IsReason(err, "leadership_fence_missing") {
		t.Fatalf("placement after the epoch ended = %s/%q, want leadership_fence_missing",
			faults.CodeOf(err), faults.ReasonOf(err))
	}
}

// The queue's own failure travels out unchanged for the same reason the facts
// source's does: it is what the repository reads to tell a transaction that
// must be replayed from one that never can be.
func TestPlacementProducerReturnsTheQueueFailureUnchanged(t *testing.T) {
	queue := newRecordingQueue()
	queue.failure = faults.New(faults.CodeUnavailable, "queue is unreachable",
		faults.WithReason("work_queue_unavailable"))
	producer, err := newPlacementProducer(queue, PlacementFactsFunc(
		func(context.Context, orchestration.WorkItem) (scheduling.AdmissionRequest, error) {
			return admissionFor(t), nil
		}))
	if err != nil {
		t.Fatalf("newPlacementProducer: %v", err)
	}
	err = producer.EnqueueStage(context.Background(), workItemFor(t))
	if !faults.IsCode(err, faults.CodeUnavailable) || !faults.IsReason(err, "work_queue_unavailable") {
		t.Fatalf("producer error = %s/%q, want the queue failure verbatim", faults.CodeOf(err), faults.ReasonOf(err))
	}
}
