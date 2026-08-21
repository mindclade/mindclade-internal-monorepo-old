// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package admissionpostgres

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"go.mindclade.dev/control/admission"
	"go.mindclade.dev/libs/go/audit"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/requestmeta"
)

type reservationEvent struct {
	SchemaVersion      int                        `json:"schema_version"`
	ReservationID      string                     `json:"reservation_id"`
	Workspace          string                     `json:"workspace"`
	Subject            string                     `json:"subject"`
	Route              admission.GatewayRoute     `json:"route"`
	PolicyEpoch        uint64                     `json:"policy_epoch"`
	EntitlementID      string                     `json:"entitlement_id"`
	EntitlementVersion string                     `json:"entitlement_version"`
	BudgetID           string                     `json:"budget_id"`
	BudgetVersion      string                     `json:"budget_version"`
	Reserved           admission.Quota            `json:"reserved"`
	RequestedTTL       string                     `json:"requested_ttl"`
	Actual             admission.Quota            `json:"actual,omitempty"`
	State              admission.ReservationState `json:"state"`
	ExpiresAt          string                     `json:"expires_at"`
	FinalizedAt        string                     `json:"finalized_at,omitempty"`
	ResourceVersion    string                     `json:"resource_version"`
}

func (store *Store) emitReservation(ctx context.Context, action string, reservation admission.Reservation) error {
	payload, err := json.Marshal(reservationEvent{
		SchemaVersion: 1, ReservationID: reservation.ID.String(), Workspace: reservation.Workspace,
		Subject: reservation.Subject, Route: reservation.Route, PolicyEpoch: reservation.PolicyEpoch,
		EntitlementID: reservation.EntitlementID.String(), EntitlementVersion: reservation.EntitlementVersion.String(),
		BudgetID: reservation.BudgetID.String(), BudgetVersion: reservation.BudgetVersion.String(),
		Reserved: reservation.Reserved.Clone(), RequestedTTL: reservation.RequestedTTL.String(),
		Actual: reservation.Actual.Clone(), State: reservation.State,
		ExpiresAt:   reservation.ExpiresAt.UTC().Format(time.RFC3339Nano),
		FinalizedAt: optionalTimestamp(reservation.FinalizedAt), ResourceVersion: reservation.Version.String(),
	})
	if err != nil {
		return internal(ctx, err, "admission.postgres.emitReservation", "admission_event_encoding_failed")
	}
	fields := audit.Fields{
		"workspace": reservation.Workspace, "subject": reservation.Subject,
		"endpoint": reservation.Route.Endpoint, "provider": reservation.Route.Provider,
		"model": reservation.Route.Model, "state": string(reservation.State),
		"policy_epoch":     strconv.FormatUint(reservation.PolicyEpoch, 10),
		"resource_version": reservation.Version.String(),
	}
	return store.emit(ctx, action, "gateway_reservation", reservation.ID, reservation.Workspace,
		"control.admission.reservation.v1", reservation.Workspace, payload, fields)
}

func (store *Store) emitPolicy(ctx context.Context, action, targetType string, id identifiers.ID, workspace, topic string, payload []byte) error {
	return store.emit(ctx, action, targetType, id, workspace, topic, workspace, payload,
		audit.Fields{"workspace": workspace})
}

func (store *Store) emit(ctx context.Context, action, targetType string, id identifiers.ID, name, topic, partitionKey string, payload []byte, fields audit.Fields) error {
	target, err := audit.NewTarget(targetType, audit.WithTargetID(id), audit.WithTargetName(name))
	if err != nil {
		return err
	}
	event, err := store.audits.Create(audit.MustParseAction(action), store.actor, target,
		audit.OutcomeSucceeded, audit.WithFields(fields))
	if err != nil {
		return err
	}
	if err := audit.Record(ctx, store.recorder, event); err != nil {
		return err
	}
	message, err := store.events.Create(topic, partitionKey, "application/json", payload,
		map[string]string{"schema-version": "1"}, requestmeta.Metadata{}, time.Time{})
	if err != nil {
		return err
	}
	return store.messages.Append(ctx, message)
}

func optionalTimestamp(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}
