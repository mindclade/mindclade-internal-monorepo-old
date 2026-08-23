// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package schedulingpostgres

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"go.mindclade.dev/control/scheduling"
	"go.mindclade.dev/libs/go/audit"
	"go.mindclade.dev/libs/go/auth"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/requestmeta"
)

const (
	// The topics are the domain's published join contract. They are versioned
	// in the name rather than only in the payload so a consumer that cannot
	// read v2 keeps reading v1 instead of silently mis-parsing it.
	ReservationEventTopic = "control.scheduling.reservation.v1"
	QuotaEventTopic       = "control.scheduling.quota.v1"
	WeightEventTopic      = "control.scheduling.share_weight.v1"

	// ReservationActionPrefix is joined with the reservation's own state to
	// form the audit action, so every lifecycle event is named after the state
	// it produced and no two call sites can name the same event differently.
	ReservationActionPrefix = "scheduling.reservation."

	ReservationTargetType = "scheduling_reservation"
	QuotaTargetType       = "scheduling_capacity_quota"
	WeightTargetType      = "scheduling_share_weight"

	// The lineage headers let a maintenance probe join the relational audit
	// columns to the outbox row without decoding either the JSON body or the
	// bytea payload. They mirror the orchestration adapter's header names on
	// purpose: one probe reads both domains.
	LineageAuditEventIDHeader    = "audit-event-id"
	LineageAuditActionHeader     = "audit-action"
	LineageTargetTypeHeader      = "audit-target-type"
	LineageTargetIDHeader        = "audit-target-id"
	LineageResourceVersionHeader = "resource-version"
	LineageSchemaVersionHeader   = "schema-version"

	EventSchemaVersion = 1
)

// authorship names who caused a transition, which is not always who asked for
// the call that applied it.
//
// Every emit in the orchestration template is the caller's action, so that
// adapter reads the principal off the context unconditionally. This package
// cannot: the expiry sweep runs inside whatever mutation happened to arrive
// first, and ledger.go argues -- correctly -- that expiry is authored by the
// deadline rather than by a leader. Attributing it to the ambient principal
// would record an operator who called Snapshot as the author of every hold that
// lapsed while they were reading, which is the one way an audit log must not be
// wrong. So the caller states the authorship and emit refuses to guess.
type authorship int

const (
	// callerAuthored: the caller asked for this transition. Their principal is
	// the actor when the context carries one.
	callerAuthored authorship = iota
	// deadlineAuthored: a deadline caused this transition and the caller merely
	// happened to be the mutation that swept it. The store's system actor is
	// the author, whatever principal is on the context.
	deadlineAuthored
)

// reservationEvent is deliberately not the reservation. A Reservation carries
// the whole sealed Placement -- the per-replica and total demand vectors, the
// topology constraint, the snapshot provenance digests -- and republishing all
// of it on every lifecycle transition would put the placement decision on the
// wire six times for one workload. What a consumer needs to follow capacity is
// who holds how much of which domain, and in what state; a consumer that needs
// the decision itself calls Get.
type reservationEvent struct {
	SchemaVersion   int               `json:"schema_version"`
	ReservationID   string            `json:"reservation_id"`
	RunID           string            `json:"run_id"`
	StageID         string            `json:"stage_id"`
	Attempt         uint32            `json:"attempt"`
	WorkloadID      string            `json:"workload_id"`
	Tenant          string            `json:"tenant"`
	Workspace       string            `json:"workspace"`
	CapacityDomain  string            `json:"capacity_domain"`
	Pool            string            `json:"pool"`
	Priority        string            `json:"priority"`
	State           string            `json:"state"`
	Sequence        uint32            `json:"sequence"`
	LeaseFence      uint64            `json:"lease_fence"`
	Total           scheduling.Demand `json:"total"`
	ExpiresAt       string            `json:"expires_at"`
	Preemptor       string            `json:"preemptor,omitempty"`
	ResourceVersion string            `json:"resource_version"`
}

type quotaEvent struct {
	SchemaVersion  int               `json:"schema_version"`
	CapacityDomain string            `json:"capacity_domain"`
	QueueName      string            `json:"queue_name"`
	Nominal        scheduling.Demand `json:"nominal"`
	Epoch          uint64            `json:"epoch"`
}

type weightEvent struct {
	SchemaVersion int    `json:"schema_version"`
	Tenant        string `json:"tenant"`
	Weight        uint32 `json:"weight"`
	Epoch         uint64 `json:"epoch"`
}

// emitReservation publishes one reservation transition.
//
// The action is derived from the sealed record's own state rather than passed
// in by the call site. Two call sites naming the same logical event differently
// is not hypothetical -- the expiry sweep and the explicit Expire transition
// did exactly that, and a consumer filtering on the action would have seen half
// the expiries. Deriving it here makes the two structurally identical: every
// reservation event is "scheduling.reservation." followed by the state the
// record is now in, and there is no second place to get it wrong.
func (store *Store) emitReservation(ctx context.Context, author authorship, record scheduling.Reservation) error {
	const operation = "scheduling.postgres.emitReservation"
	action := ReservationActionPrefix + string(record.State)
	payload, err := json.Marshal(reservationEvent{
		SchemaVersion:   EventSchemaVersion,
		ReservationID:   record.ID.String(),
		RunID:           record.Placement.RunID,
		StageID:         record.Placement.StageID,
		Attempt:         record.Placement.Attempt,
		WorkloadID:      record.Placement.WorkloadID.String(),
		Tenant:          record.Placement.Tenant,
		Workspace:       record.Placement.Workspace,
		CapacityDomain:  string(record.Placement.Pool.Domain.WorkloadClass()),
		Pool:            string(record.Placement.Pool.Kind),
		Priority:        string(record.Placement.Priority),
		State:           string(record.State),
		Sequence:        record.Sequence,
		LeaseFence:      record.LeaseFence,
		Total:           record.Placement.Total.Clone(),
		ExpiresAt:       record.ExpiresAt.UTC().Format(time.RFC3339Nano),
		Preemptor:       record.Preemptor.String(),
		ResourceVersion: record.Version.String(),
	})
	if err != nil {
		return internal(ctx, err, operation, "scheduling_event_encoding_failed")
	}
	fields := audit.Fields{
		"reservation_id":   record.ID.String(),
		"run_id":           record.Placement.RunID,
		"stage_id":         record.Placement.StageID,
		"tenant":           record.Placement.Tenant,
		"capacity_domain":  string(record.Placement.Pool.Domain.WorkloadClass()),
		"state":            string(record.State),
		"lease_fence":      strconv.FormatUint(record.LeaseFence, 10),
		"resource_version": record.Version.String(),
	}
	// The run is the partition key rather than the reservation. Ordering has to
	// hold within one run -- a consumer rebuilding a run's capacity story reads
	// its stages' reservations against each other -- and a reservation's own
	// transitions are a subsequence of its run's stream, so per-reservation
	// order is preserved by the stronger guarantee rather than given up for it.
	return store.emit(ctx, author, action, ReservationTargetType, record.ID, record.Placement.RunID,
		ReservationEventTopic, record.Placement.RunID, record.Version.String(), payload, fields)
}

func (store *Store) emitQuota(ctx context.Context, domain scheduling.CapacityDomain, nominal scheduling.Demand, epoch uint64) error {
	const operation = "scheduling.postgres.emitQuota"
	class := string(domain.WorkloadClass())
	payload, err := json.Marshal(quotaEvent{
		SchemaVersion: EventSchemaVersion, CapacityDomain: class,
		QueueName: domain.QueueName(), Nominal: nominal.Clone(), Epoch: epoch,
	})
	if err != nil {
		return internal(ctx, err, operation, "scheduling_event_encoding_failed")
	}
	fields := audit.Fields{
		"capacity_domain": class,
		"queue_name":      domain.QueueName(),
		"epoch":           strconv.FormatUint(epoch, 10),
	}
	// A capacity domain has no identifiers.ID -- it is a configuration key, not
	// a resource with a lifecycle -- so the audit target names it and carries a
	// zero id, which audit.Target explicitly permits for configuration-level
	// operations.
	return store.emit(ctx, callerAuthored, "scheduling.quota.put", QuotaTargetType, identifiers.ID{}, class,
		QuotaEventTopic, class, "", payload, fields)
}

func (store *Store) emitWeight(ctx context.Context, tenant string, weight uint32, epoch uint64) error {
	const operation = "scheduling.postgres.emitWeight"
	payload, err := json.Marshal(weightEvent{
		SchemaVersion: EventSchemaVersion, Tenant: tenant, Weight: weight, Epoch: epoch,
	})
	if err != nil {
		return internal(ctx, err, operation, "scheduling_event_encoding_failed")
	}
	fields := audit.Fields{
		"tenant": tenant,
		"weight": strconv.FormatUint(uint64(weight), 10),
		"epoch":  strconv.FormatUint(epoch, 10),
	}
	return store.emit(ctx, callerAuthored, "scheduling.share_weight.put", WeightTargetType, identifiers.ID{}, tenant,
		WeightEventTopic, tenant, "", payload, fields)
}

// emit appends the audit record and the outbox message. Both use the ambient
// transaction, so this call is part of the caller's commit rather than a
// separate write that a crash could leave half-applied.
func (store *Store) emit(
	ctx context.Context, author authorship, action, targetType string, id identifiers.ID, name,
	topic, partitionKey, resourceVersion string, payload []byte, fields audit.Fields,
) error {
	options := []audit.TargetOption{audit.WithTargetName(name)}
	if !id.IsZero() {
		options = append(options, audit.WithTargetID(id))
	}
	target, err := audit.NewTarget(targetType, options...)
	if err != nil {
		return err
	}
	// The ambient principal is consulted only for a transition the caller
	// actually caused. A deadline-authored one keeps the store's system actor
	// even when a principal is present, because the caller did not cause it --
	// they merely ran the mutation that noticed.
	actor := store.actor
	if author == callerAuthored {
		if principal, ok := auth.PrincipalFromContext(ctx); ok {
			actor, err = audit.ActorFromPrincipal(principal)
			if err != nil {
				return err
			}
		}
	}
	metadata, hasMetadata := requestmeta.FromContext(ctx)
	eventOptions := []audit.EventOption{audit.WithFields(fields)}
	if hasMetadata {
		eventOptions = append(eventOptions, audit.WithRequestMetadata(metadata))
	}
	event, err := store.audits.Create(audit.MustParseAction(action), actor, target,
		audit.OutcomeSucceeded, eventOptions...)
	if err != nil {
		return err
	}
	headers := map[string]string{
		LineageSchemaVersionHeader: strconv.Itoa(EventSchemaVersion),
		LineageAuditEventIDHeader:  event.ID().String(),
		LineageAuditActionHeader:   event.Action().String(),
		LineageTargetTypeHeader:    target.Type(),
	}
	// A quota and a weight are configuration facts and carry neither an id nor
	// a resource version. Emitting either header with an empty value would make
	// a lineage probe unable to tell "no version" from "version lost".
	if !id.IsZero() {
		headers[LineageTargetIDHeader] = id.String()
	}
	if resourceVersion != "" {
		headers[LineageResourceVersionHeader] = resourceVersion
	}
	if err := audit.Record(ctx, store.recorder, event); err != nil {
		return err
	}
	message, err := store.events.Create(topic, partitionKey, "application/json", payload,
		headers, metadata, time.Time{})
	if err != nil {
		return err
	}
	return store.messages.Append(ctx, message)
}
