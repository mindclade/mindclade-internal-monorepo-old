// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"

	"mindclade.internal/libs/go/faults"
)

type stateError string

func (value stateError) Error() string    { return string(value) }
func (value stateError) SQLState() string { return string(value) }

func TestQualifySQLState(t *testing.T) {
	tests := []struct {
		state string
		code  faults.Code
		retry bool
	}{
		{"23505", faults.CodeAlreadyExists, false},
		{"23503", faults.CodeFailedPrecondition, false},
		{"22012", faults.CodeInvalidArgument, false},
		{"42501", faults.CodePermissionDenied, false},
		{"28P01", faults.CodeUnauthenticated, false},
		{"40001", faults.CodeAborted, true},
		{"40003", faults.CodeAborted, true},
		{"08006", faults.CodeUnavailable, true},
		{"08099", faults.CodeUnavailable, true},
		{"53000", faults.CodeResourceExhausted, true},
		{"99999", faults.CodeInternal, false},
	}
	for _, test := range tests {
		t.Run(test.state, func(t *testing.T) {
			err := Qualify(context.Background(), stateError(test.state), "test")
			if !faults.IsCode(err, test.code) || faults.IsRetryable(err) != test.retry {
				t.Fatalf("state %s => %v", test.state, err)
			}
			if got := faults.FieldsOf(err)["postgres_sqlstate"]; got != test.state {
				t.Fatalf("sqlstate field = %v, want %q", got, test.state)
			}
		})
	}
}

func TestQualifyStandardErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		code faults.Code
	}{
		{"canceled", context.Canceled, faults.CodeCanceled},
		{"deadline", context.DeadlineExceeded, faults.CodeDeadlineExceeded},
		{"not found", sql.ErrNoRows, faults.CodeNotFound},
		{"bad connection", driver.ErrBadConn, faults.CodeUnavailable},
		{"unknown driver", errors.New("driver failure"), faults.CodeUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			qualified := Qualify(context.Background(), test.err, "")
			if !faults.IsCode(qualified, test.code) {
				t.Fatalf("code = %s, want %s: %v", faults.CodeOf(qualified), test.code, qualified)
			}
			if !errors.Is(qualified, test.err) {
				t.Fatal("cause not preserved")
			}
		})
	}
}

func TestQualifyQueryCanceledUsesContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Qualify(ctx, stateError("57014"), "query"); !faults.IsCode(err, faults.CodeCanceled) {
		t.Fatalf("canceled query = %v", err)
	}

	if err := Qualify(context.Background(), stateError("57014"), "query"); !faults.IsCode(err, faults.CodeAborted) {
		t.Fatalf("server-canceled query = %v", err)
	}
}

func TestQualifyPreservesExistingFaultAndNil(t *testing.T) {
	existing := faults.New(faults.CodeConflict, "conflict")
	if got := Qualify(context.Background(), existing, "test"); got != existing {
		t.Fatal("existing fault was replaced")
	}
	if Qualify(context.Background(), nil, "test") != nil {
		t.Fatal("nil error changed")
	}
}
