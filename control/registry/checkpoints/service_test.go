// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package checkpoints

import (
	"context"
	"testing"
	"time"

	"go.mindclade.dev/control/artifacts"
	"go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/identifiers"
)

var registeredAt = time.UnixMilli(1800000000000).UTC()

type memoryRepository struct {
	records map[string]Record
	tamper  func(Record) Record
}

type checkpointVerifierFunc func(context.Context, Record) (VerifiedPublication, error)

func (function checkpointVerifierFunc) VerifyCommittedCheckpoint(ctx context.Context, record Record) (VerifiedPublication, error) {
	return function(ctx, record)
}

type publicationAuthorityFunc func(context.Context, Record, VerifiedPublication) error

func (function publicationAuthorityFunc) AuthorizeRegistration(ctx context.Context, record Record, verified VerifiedPublication) error {
	return function(ctx, record, verified)
}

func newRepository() *memoryRepository {
	return &memoryRepository{records: make(map[string]Record)}
}

func (r *memoryRepository) Create(_ context.Context, record Record) error {
	if _, exists := r.records[record.CheckpointID]; exists {
		return invalid("checkpoint_already_exists", "checkpoint already exists", nil)
	}
	r.records[record.CheckpointID] = record
	return nil
}

func (r *memoryRepository) Get(_ context.Context, checkpointID string) (Record, error) {
	record, exists := r.records[checkpointID]
	if !exists {
		return Record{}, invalid("checkpoint_not_found", "checkpoint was not found", nil)
	}
	if r.tamper != nil {
		return r.tamper(record), nil
	}
	return record, nil
}

func (r *memoryRepository) CompareAndSwap(_ context.Context, record Record, expected uint64) error {
	current, exists := r.records[record.CheckpointID]
	if !exists || current.ResourceVersion != expected {
		return invalid("checkpoint_repository_conflict", "checkpoint repository compare-and-swap failed", nil)
	}
	r.records[record.CheckpointID] = record
	return nil
}

func artifact(seed, kind string, size uint64) artifacts.Ref {
	return artifacts.Ref{
		Digest:        identifiers.SHA256String(seed),
		SizeBytes:     size,
		MediaType:     "application/vnd.mindclade." + kind + ".v1+proto",
		LogicalKind:   kind,
		SchemaVersion: 1,
	}
}

func validRecord() Record {
	return Record{
		CheckpointID:        "checkpoint_019c0000000070008000000000000001",
		RunID:               "run_019c0000000070008000000000000002",
		Manifest:            artifact("manifest", manifestLogicalKind, 2048),
		Commit:              artifact("commit", commitLogicalKind, 512),
		OptimizerStep:       42,
		CheckpointAttempt:   2,
		FencingToken:        9,
		TopologyFingerprint: identifiers.SHA256String("topology"),
		Lifecycle:           LifecycleCommitted,
		Created:             registeredAt.Add(-time.Minute),
		RetainUntil:         registeredAt.Add(24 * time.Hour),
		PolicyEpoch:         3,
		ResourceVersion:     1,
	}
}

func verifiedPublication(record Record) VerifiedPublication {
	return VerifiedPublication{
		CheckpointID:        record.CheckpointID,
		RunID:               record.RunID,
		StageAttempt:        1,
		CheckpointAttempt:   record.CheckpointAttempt,
		FencingToken:        record.FencingToken,
		Microbatches:        84,
		OptimizerStep:       record.OptimizerStep,
		Samples:             168,
		DataPosition:        168,
		Manifest:            record.Manifest,
		Commit:              record.Commit,
		TopologyFingerprint: record.TopologyFingerprint,
		ParentManifest:      record.ParentManifest,
	}
}

func newService(repository Repository, now time.Time) Service {
	policy := ProductionPolicy()
	policy.ActivePolicyEpoch = validRecord().PolicyEpoch
	return Service{
		Repository: repository,
		Policy:     policy,
		Clock:      clock.NewFake(now),
		Verifier: checkpointVerifierFunc(func(_ context.Context, record Record) (VerifiedPublication, error) {
			return verifiedPublication(record), nil
		}),
		Authority: publicationAuthorityFunc(func(_ context.Context, _ Record, _ VerifiedPublication) error {
			return nil
		}),
	}
}

func TestRegisterSealsAndResolveVerifiesRecord(t *testing.T) {
	repository := newRepository()
	service := newService(repository, registeredAt)
	registered, err := service.Register(context.Background(), validRecord())
	if err != nil {
		t.Fatal(err)
	}
	if !registered.RecordDigest.Valid() {
		t.Fatal("register did not seal the checkpoint record")
	}
	resolved, err := service.Resolve(context.Background(), registered.CheckpointID)
	if err != nil {
		t.Fatal(err)
	}
	if !resolved.RecordDigest.Equal(registered.RecordDigest) {
		t.Fatal("resolve returned a different checkpoint record")
	}
}

func TestResolveRejectsTamperedAndExpiredRecords(t *testing.T) {
	repository := newRepository()
	service := newService(repository, registeredAt)
	registered, err := service.Register(context.Background(), validRecord())
	if err != nil {
		t.Fatal(err)
	}
	repository.tamper = func(record Record) Record {
		record.OptimizerStep++
		return record
	}
	if _, err = service.Resolve(context.Background(), registered.CheckpointID); err == nil {
		t.Fatal("expected tampered checkpoint record to fail verification")
	}
	repository.tamper = nil
	expiredService := newService(repository, registered.RetainUntil)
	if _, err = expiredService.Resolve(context.Background(), registered.CheckpointID); err == nil {
		t.Fatal("expected elapsed retention to make checkpoint non-restorable")
	}
}

func TestLifecycleTransitionsUseOptimisticConcurrency(t *testing.T) {
	repository := newRepository()
	service := newService(repository, registeredAt)
	registered, err := service.Register(context.Background(), validRecord())
	if err != nil {
		t.Fatal(err)
	}
	protected, err := service.Transition(context.Background(), registered.CheckpointID, LifecycleProtected, 1)
	if err != nil {
		t.Fatal(err)
	}
	if protected.ResourceVersion != 2 || protected.Lifecycle != LifecycleProtected {
		t.Fatalf("unexpected protected record: %#v", protected)
	}
	if _, err = service.Transition(context.Background(), registered.CheckpointID, LifecycleRevoked, 1); err == nil {
		t.Fatal("expected stale resource version to fail")
	}
	revoked, err := service.Transition(context.Background(), registered.CheckpointID, LifecycleRevoked, 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Resolve(context.Background(), revoked.CheckpointID); err == nil {
		t.Fatal("expected revoked checkpoint to be non-restorable")
	}
}

func TestExpiryRequiresElapsedRetention(t *testing.T) {
	repository := newRepository()
	service := newService(repository, registeredAt)
	registered, err := service.Register(context.Background(), validRecord())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Transition(context.Background(), registered.CheckpointID, LifecycleExpired, 1); err == nil {
		t.Fatal("expected early expiry to fail")
	}
	service = newService(repository, registered.RetainUntil)
	if _, err = service.Transition(context.Background(), registered.CheckpointID, LifecycleExpired, 1); err != nil {
		t.Fatal(err)
	}
}

func TestRegistrationRejectsInvalidAuthorityAndBounds(t *testing.T) {
	tests := map[string]func(*Record){
		"wrong manifest kind": func(record *Record) {
			record.Manifest.LogicalKind = "other"
		},
		"non-canonical media type": func(record *Record) {
			record.Manifest.MediaType = "application/protobuf|alias"
		},
		"same manifest and commit": func(record *Record) {
			record.Commit.Digest = record.Manifest.Digest
		},
		"zero fence": func(record *Record) {
			record.FencingToken = 0
		},
		"wrong policy epoch": func(record *Record) {
			record.PolicyEpoch++
		},
		"short retention": func(record *Record) {
			record.RetainUntil = record.Created.Add(time.Minute)
		},
		"non-canonical timestamp precision": func(record *Record) {
			record.Created = record.Created.Add(time.Nanosecond)
		},
		"retention already elapsed": func(record *Record) {
			record.Created = registeredAt.Add(-2 * time.Hour)
			record.RetainUntil = registeredAt.Add(-time.Hour)
		},
		"oversized manifest": func(record *Record) {
			record.Manifest.SizeBytes = ProductionPolicy().MaximumManifest + 1
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			record := validRecord()
			mutate(&record)
			if _, err := newService(newRepository(), registeredAt).Register(context.Background(), record); err == nil {
				t.Fatal("expected invalid checkpoint record to be rejected")
			}
		})
	}
}

func TestRegisterRequiresVerifiedBytesAndCurrentAuthority(t *testing.T) {
	repository := newRepository()
	service := newService(repository, registeredAt)
	service.Verifier = checkpointVerifierFunc(func(_ context.Context, record Record) (VerifiedPublication, error) {
		verified := verifiedPublication(record)
		verified.OptimizerStep++
		return verified, nil
	})
	if _, err := service.Register(context.Background(), validRecord()); err == nil {
		t.Fatal("expected verified byte facts that differ from the record to fail")
	}
	service = newService(repository, registeredAt)
	service.Authority = publicationAuthorityFunc(func(_ context.Context, _ Record, _ VerifiedPublication) error {
		return invalid("checkpoint_stale_fence", "checkpoint fence is no longer current", nil)
	})
	if _, err := service.Register(context.Background(), validRecord()); err == nil {
		t.Fatal("expected current publication authority rejection to fail registration")
	}
	if len(repository.records) != 0 {
		t.Fatal("failed verification or authority must not create a registry record")
	}
}

func TestParentManifestIsCompleteAndAcyclic(t *testing.T) {
	record := validRecord()
	record.ParentManifest = record.Manifest
	if err := record.Validate(); err == nil {
		t.Fatal("expected self-parent checkpoint to be rejected")
	}
	record.ParentManifest = artifacts.Ref{SizeBytes: 1}
	if err := record.Validate(); err == nil {
		t.Fatal("expected incomplete parent checkpoint to be rejected")
	}
}

func TestServiceRequiresProviders(t *testing.T) {
	service := newService(newRepository(), registeredAt)
	service.Repository = nil
	if _, err := service.Register(context.Background(), validRecord()); err == nil {
		t.Fatal("expected missing repository to fail")
	}
	service = newService(newRepository(), registeredAt)
	service.Clock = nil
	if _, err := service.Register(context.Background(), validRecord()); err == nil {
		t.Fatal("expected missing clock to fail")
	}
	service = newService(newRepository(), registeredAt)
	service.Verifier = nil
	if _, err := service.Register(context.Background(), validRecord()); err == nil {
		t.Fatal("expected missing checkpoint verifier to fail")
	}
	service = newService(newRepository(), registeredAt)
	service.Authority = nil
	if _, err := service.Register(context.Background(), validRecord()); err == nil {
		t.Fatal("expected missing publication authority to fail")
	}
	service = newService(newRepository(), registeredAt)
	service.Policy.ActivePolicyEpoch = 0
	if _, err := service.Register(context.Background(), validRecord()); err == nil {
		t.Fatal("expected unconfigured active policy epoch to fail")
	}
}
