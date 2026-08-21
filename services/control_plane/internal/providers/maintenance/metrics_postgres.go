// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package maintenance

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"go.mindclade.dev/libs/go/faults"
	sqlpostgres "go.mindclade.dev/libs/go/storage/sql/postgres"
	admissionstore "go.mindclade.dev/services/control_plane/internal/store/postgres/admission"
)

const maximumHousekeepingResultBytes = 1024

type maintenanceMetricTables struct {
	reservations string
	audit        string
	outbox       string
	workQueue    string
}

type postgresMaintenanceSnapshotSource struct {
	db       *sql.DB
	tables   maintenanceMetricTables
	lookback time.Duration
	limit    int
}

func newPostgresMaintenanceSnapshotSource(db *sql.DB, tables maintenanceMetricTables) (*postgresMaintenanceSnapshotSource, error) {
	const operation = "controlplane.maintenance.newPostgresMaintenanceSnapshotSource"
	if db == nil {
		return nil, maintenanceFault(faults.CodeInvalidArgument, "maintenance_metrics_database_missing", "maintenance metrics database is required", operation)
	}
	for _, table := range []string{tables.reservations, tables.audit, tables.outbox, tables.workQueue} {
		if !sqlpostgres.ValidQualifiedIdentifier(table) {
			return nil, maintenanceFault(faults.CodeInvalidArgument, "maintenance_metrics_table_invalid", "maintenance metrics table name is invalid", operation)
		}
	}
	return &postgresMaintenanceSnapshotSource{db: db, tables: tables, lookback: maintenanceDriftLookback, limit: maintenanceDriftLimit}, nil
}

func (source *postgresMaintenanceSnapshotSource) Expiration(ctx context.Context, now time.Time) (maintenanceExpirationSnapshot, error) {
	const operation = "controlplane.maintenance.metrics.Expiration"
	if ctx == nil || source == nil || source.db == nil || now.IsZero() {
		return maintenanceExpirationSnapshot{}, maintenanceFault(faults.CodeInvalidArgument, "maintenance_expiration_probe_invalid", "maintenance expiration probe request is invalid", operation)
	}
	if err := ctx.Err(); err != nil {
		return maintenanceExpirationSnapshot{}, err
	}

	result := maintenanceExpirationSnapshot{}
	oldestQuery := fmt.Sprintf(`WITH expired AS MATERIALIZED (
    SELECT expires_at,reservation_id FROM %s
    WHERE state='reserved' AND expires_at <= $1
    ORDER BY expires_at,reservation_id LIMIT $2
)
SELECT count(*),min(expires_at) FROM expired`, source.tables.reservations)
	var oldest sql.NullTime
	err := source.db.QueryRowContext(ctx, oldestQuery, now.Round(0).UTC(), expirationBacklogOverflowSentinel).Scan(&result.backlog, &oldest)
	if err != nil {
		return maintenanceExpirationSnapshot{}, qualifyMaintenanceProbe(ctx, err, operation+".OldestExpired")
	}
	if oldest.Valid {
		result.oldestExpiredAt = oldest.Time.Round(0).UTC()
	}

	// Only the newest two successful rows are decoded. Migration v14's
	// completed-only queue/completed_at/item_id partial index makes this
	// bounded; the result bytea is never searched or scanned to locate
	// candidates.
	sweepQuery := fmt.Sprintf(`SELECT completed_at,
CASE WHEN octet_length(result_payload) <= $2 THEN result_payload END
FROM %s
WHERE queue=$1 AND state='completed'
ORDER BY completed_at DESC,item_id DESC LIMIT 2`, source.tables.workQueue)
	rows, err := source.db.QueryContext(ctx, sweepQuery, housekeepingQueue, maximumHousekeepingResultBytes)
	if err != nil {
		return maintenanceExpirationSnapshot{}, qualifyMaintenanceProbe(ctx, err, operation+".Sweeps")
	}
	defer rows.Close()
	for rows.Next() {
		var completedAt time.Time
		var payload []byte
		if err := rows.Scan(&completedAt, &payload); err != nil {
			return maintenanceExpirationSnapshot{}, qualifyMaintenanceProbe(ctx, err, operation+".SweepScan")
		}
		decoded, err := decodeHousekeepingResult(payload)
		if err != nil {
			return maintenanceExpirationSnapshot{}, faults.Wrap(err, faults.CodeDataLoss, "completed housekeeping result is invalid",
				faults.WithReason("maintenance_sweep_result_invalid"), faults.WithOperation(operation), faults.WithRetryPolicy(faults.NoRetry()))
		}
		if result.lastSuccessfulSweep.IsZero() {
			result.lastSuccessfulSweep = completedAt.Round(0).UTC()
		}
		if decoded.Backlog && result.consecutiveBackloggedSweeps < 2 {
			result.consecutiveBackloggedSweeps++
			continue
		}
		break
	}
	if err := rows.Err(); err != nil {
		return maintenanceExpirationSnapshot{}, qualifyMaintenanceProbe(ctx, err, operation+".SweepRows")
	}
	return result, nil
}

func (source *postgresMaintenanceSnapshotSource) Lineage(ctx context.Context, now time.Time) (maintenanceLineageSnapshot, error) {
	const operation = "controlplane.maintenance.metrics.Lineage"
	if ctx == nil || source == nil || source.db == nil || now.IsZero() || source.lookback <= 0 || source.limit <= 0 || source.limit > maintenanceDriftLimit {
		return maintenanceLineageSnapshot{}, maintenanceFault(faults.CodeInvalidArgument, "maintenance_lineage_probe_invalid", "maintenance lineage probe request is invalid", operation)
	}
	if err := ctx.Err(); err != nil {
		return maintenanceLineageSnapshot{}, err
	}
	// This is deliberately a bounded recent sample, not an exhaustive history
	// scan. Each side contributes at most limit candidates from an indexed
	// lookback window. Drift is an invariant violation and is therefore retained
	// in the exported last-known snapshot until a later sample proves it absent.
	query := fmt.Sprintf(`WITH recent_outbox_base AS MATERIALIZED (
    SELECT message_id,
           created_at,
           headers->>'%s' AS schema_version,
           NULLIF(headers->>'%s','') AS audit_event_id,
           headers->>'%s' AS audit_action,
           headers->>'%s' AS audit_target_type,
           headers->>'%s' AS audit_target_id,
           headers->>'%s' AS resource_version
    FROM %s
    WHERE topic='%s' AND created_at >= $1
    ORDER BY created_at DESC,message_id DESC
    LIMIT $2
), recent_outbox AS MATERIALIZED (
    SELECT *,row_number() OVER (
        PARTITION BY audit_target_id ORDER BY created_at DESC,message_id DESC
    ) AS target_event_rank
    FROM recent_outbox_base
), recent_audit AS MATERIALIZED (
    SELECT event_id,action,target_type,target_id
    FROM %s
    WHERE target_type='%s' AND occurred_at >= $1
    ORDER BY occurred_at DESC,event_id DESC
    LIMIT $2
), compared AS MATERIALIZED (
    SELECT o.schema_version,o.audit_event_id,o.audit_action,o.audit_target_type,o.audit_target_id,
           o.resource_version,o.target_event_rank,
           a.event_id,a.action,a.target_type,a.target_id,
           r.reservation_id,r.resource_version AS ledger_resource_version
    FROM recent_outbox o
    LEFT JOIN %s a ON a.event_id=o.audit_event_id
    LEFT JOIN %s r ON r.reservation_id=o.audit_target_id
)
SELECT
    (SELECT count(*) FROM compared WHERE event_id IS NULL),
    (SELECT count(*) FROM recent_audit a WHERE NOT EXISTS (
        SELECT 1 FROM %s o
        WHERE o.topic='%s'
          AND o.headers ? '%s'
          AND o.headers->>'%s'=a.event_id
    )),
    (SELECT count(*) FROM compared
     WHERE event_id IS NOT NULL AND (
         schema_version IS DISTINCT FROM '%d' OR
         audit_action IS DISTINCT FROM action OR
         audit_target_type IS DISTINCT FROM target_type OR
         audit_target_id IS DISTINCT FROM target_id OR
         resource_version IS NULL OR
         resource_version !~ '^rv1:[1-9][0-9]*:sha256:[0-9a-f]{64}$' OR
         (target_event_rank=1 AND (
             reservation_id IS NULL OR resource_version IS DISTINCT FROM ledger_resource_version
         ))
     ))`,
		admissionstore.LineageSchemaVersionHeader,
		admissionstore.LineageAuditEventIDHeader,
		admissionstore.LineageAuditActionHeader,
		admissionstore.LineageTargetTypeHeader,
		admissionstore.LineageTargetIDHeader,
		admissionstore.LineageResourceVersionHeader,
		source.tables.outbox,
		admissionstore.ReservationEventTopic,
		source.tables.audit,
		admissionstore.ReservationTargetType,
		source.tables.audit,
		source.tables.reservations,
		source.tables.outbox,
		admissionstore.ReservationEventTopic,
		admissionstore.LineageAuditEventIDHeader,
		admissionstore.LineageAuditEventIDHeader,
		admissionstore.ReservationEventSchemaVersion,
	)
	var result maintenanceLineageSnapshot
	err := source.db.QueryRowContext(ctx, query, now.Round(0).UTC().Add(-source.lookback), source.limit).Scan(
		&result.missingAudit, &result.missingOutbox, &result.mismatch,
	)
	if err != nil {
		return maintenanceLineageSnapshot{}, qualifyMaintenanceProbe(ctx, err, operation)
	}
	return result, nil
}

func decodeHousekeepingResult(payload []byte) (housekeepingResult, error) {
	var result housekeepingResult
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return housekeepingResult{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return housekeepingResult{}, errors.New("unexpected trailing housekeeping result")
		}
		return housekeepingResult{}, err
	}
	if result.SchemaVersion != housekeepingSchemaVersion || result.Operation != expireReservationsOperation || result.Expired < 0 {
		return housekeepingResult{}, errors.New("unsupported housekeeping result")
	}
	return result, nil
}

func qualifyMaintenanceProbe(ctx context.Context, err error, operation string) error {
	qualified := sqlpostgres.Qualify(ctx, err, operation)
	return faults.Wrap(qualified, faults.CodeOf(qualified), "maintenance admission snapshot query failed",
		faults.WithReason("maintenance_snapshot_query_failed"), faults.WithOperation(operation),
		faults.WithRetryPolicy(faults.RetryPolicyOf(qualified)), faults.WithContextMetadata(ctx))
}

var _ maintenanceSnapshotSource = (*postgresMaintenanceSnapshotSource)(nil)
