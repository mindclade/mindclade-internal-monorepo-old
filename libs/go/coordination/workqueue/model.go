// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package workqueue

import (
	"encoding/json"
	"go.mindclade.dev/libs/go/coordination"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/requestmeta"
	"strings"
	"time"
)

const (
	ItemIDKind          identifiers.Kind = "work"
	MaximumQueueBytes                    = 128
	MaximumPayloadBytes                  = 4 * 1024 * 1024
	MaximumResultBytes                   = 4 * 1024 * 1024
	MaximumReasonBytes                   = 1024
	MaximumAttempts     uint32           = 1000
)

type State string

const (
	StatePending   State = "pending"
	StateLeased    State = "leased"
	StateCompleted State = "completed"
	StateFailed    State = "failed"
	StateCancelled State = "cancelled"
)

func (state State) Valid() bool {
	switch state {
	case StatePending, StateLeased, StateCompleted, StateFailed, StateCancelled:
		return true
	}
	return false
}
func (state State) Terminal() bool {
	return state == StateCompleted || state == StateFailed || state == StateCancelled
}

type Item struct {
	ID          identifiers.ID       `json:"id"`
	Queue       string               `json:"queue"`
	Payload     json.RawMessage      `json:"payload"`
	Priority    int32                `json:"priority"`
	AvailableAt time.Time            `json:"available_at"`
	MaxAttempts uint32               `json:"max_attempts"`
	CreatedAt   time.Time            `json:"created_at"`
	Request     requestmeta.Metadata `json:"request,omitempty"`
}

func NewItem(queue string, payload json.RawMessage, priority int32, availableAt time.Time, maxAttempts uint32, request requestmeta.Metadata) (Item, error) {
	id, err := identifiers.NewID(ItemIDKind)
	if err != nil {
		return Item{}, err
	}
	return ItemFromID(id, queue, payload, priority, availableAt, maxAttempts, request)
}
func ItemFromID(id identifiers.ID, queue string, payload json.RawMessage, priority int32, availableAt time.Time, maxAttempts uint32, request requestmeta.Metadata) (Item, error) {
	if maxAttempts == 0 {
		maxAttempts = 10
	}
	now := time.Now().UTC()
	if availableAt.IsZero() {
		availableAt = now
	}
	value := Item{ID: id, Queue: strings.TrimSpace(queue), Payload: append(json.RawMessage(nil), payload...), Priority: priority, AvailableAt: availableAt.Round(0).UTC(), MaxAttempts: maxAttempts, CreatedAt: now, Request: request}
	if err := value.Validate(); err != nil {
		return Item{}, err
	}
	return value, nil
}
func (item Item) Validate() error {
	if item.ID.IsZero() || item.ID.Kind() != ItemIDKind || item.ID.Validate() != nil || item.Queue == "" || item.Queue != strings.TrimSpace(item.Queue) || len(item.Queue) > MaximumQueueBytes || len(item.Payload) == 0 || len(item.Payload) > MaximumPayloadBytes || !json.Valid(item.Payload) || item.AvailableAt.IsZero() || item.CreatedAt.IsZero() || item.MaxAttempts == 0 || item.MaxAttempts > MaximumAttempts {
		return invalid("invalid_work_item", "invalid work item", "workqueue.Item.Validate")
	}
	if !item.Request.IsZero() {
		if err := item.Request.Validate(); err != nil {
			return invalid("invalid_work_request_metadata", "invalid work request metadata", "workqueue.Item.Validate")
		}
	}
	return nil
}
func (item Item) Clone() Item {
	item.Payload = append(json.RawMessage(nil), item.Payload...)
	return item
}

type Result struct {
	ContentType string `json:"content_type"`
	Payload     []byte `json:"payload,omitempty"`
}

func (result Result) Validate() error {
	if len(result.Payload) > MaximumResultBytes || result.ContentType == "" && len(result.Payload) > 0 {
		return invalid("invalid_work_result", "invalid work result", "workqueue.Result.Validate")
	}
	return nil
}

type Record struct {
	Item        Item      `json:"item"`
	State       State     `json:"state"`
	Attempts    uint32    `json:"attempts"`
	Fence       uint64    `json:"fence"`
	UpdatedAt   time.Time `json:"updated_at"`
	CompletedAt time.Time `json:"completed_at,omitempty"`
	Result      Result    `json:"result,omitempty"`
	LastError   string    `json:"last_error,omitempty"`
}

func (record Record) Validate() error {
	if err := record.Item.Validate(); err != nil {
		return err
	}
	if !record.State.Valid() || record.UpdatedAt.IsZero() || record.Attempts > record.Item.MaxAttempts || len(record.LastError) > MaximumReasonBytes {
		return invalid("invalid_work_record", "invalid work record", "workqueue.Record.Validate")
	}
	if record.State == StateLeased && record.Fence == 0 {
		return invalid("invalid_work_fence", "leased work has no fence", "workqueue.Record.Validate")
	}
	if record.State == StateCompleted {
		if err := record.Result.Validate(); err != nil {
			return err
		}
	}
	return nil
}
func (record Record) Clone() Record {
	record.Item = record.Item.Clone()
	record.Result.Payload = append([]byte(nil), record.Result.Payload...)
	return record
}

type Claim struct {
	Record    Record             `json:"record"`
	Ownership coordination.Claim `json:"ownership"`
}

func (claim Claim) Validate() error {
	if err := claim.Record.Validate(); err != nil {
		return err
	}
	if claim.Record.State != StateLeased || claim.Record.Fence != claim.Ownership.Fence || claim.Record.Item.ID != claim.Ownership.ResourceID {
		return invalid("invalid_work_claim", "invalid work claim", "workqueue.Claim.Validate")
	}
	return claim.Ownership.Validate()
}
func (claim Claim) Item() Item { return claim.Record.Item.Clone() }
func invalid(reason, message, op string) error {
	return faults.Wrap(ErrInvalidRequest, faults.CodeInvalidArgument, message, faults.WithReason(reason), faults.WithOperation(op), faults.WithRetryPolicy(faults.NoRetry()))
}
