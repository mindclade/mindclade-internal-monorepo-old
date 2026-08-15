// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/storage/sql/sqltest"
)

func TestPoolConfigValidate(t *testing.T) {
	valid := PoolConfig{
		MaxOpenConnections:    10,
		MaxIdleConnections:    5,
		ConnectionMaxLifetime: time.Hour,
		ConnectionMaxIdleTime: time.Minute,
		PingTimeout:           time.Second,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid config: %v", err)
	}

	invalid := []PoolConfig{
		{MaxOpenConnections: -1},
		{MaxIdleConnections: -1},
		{MaxOpenConnections: 1, MaxIdleConnections: 2},
		{ConnectionMaxLifetime: -1},
		{ConnectionMaxIdleTime: -1},
		{PingTimeout: -1},
	}
	for index, config := range invalid {
		if err := config.Validate(); !faults.IsCode(err, faults.CodeInvalidArgument) {
			t.Fatalf("invalid config %d = %v", index, err)
		}
	}
}

func TestConfigureAndPing(t *testing.T) {
	state := &sqltest.State{}
	db, err := sqltest.Open(state)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	config := PoolConfig{MaxOpenConnections: 7, MaxIdleConnections: 3, PingTimeout: time.Second}
	if err := ConfigureAndPing(context.Background(), db, config); err != nil {
		t.Fatalf("ConfigureAndPing: %v", err)
	}
	if got := db.Stats().MaxOpenConnections; got != 7 {
		t.Fatalf("max open = %d, want 7", got)
	}
	if got := state.Pings.Load(); got != 1 {
		t.Fatalf("pings = %d, want 1", got)
	}
}

func TestPingQualifiesFailure(t *testing.T) {
	cause := errors.New("network unavailable")
	state := &sqltest.State{Ping: func(context.Context) error { return cause }}
	db, err := sqltest.Open(state)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	err = Ping(context.Background(), db, 0)
	if !faults.IsCode(err, faults.CodeUnavailable) || !errors.Is(err, cause) {
		t.Fatalf("Ping = %v", err)
	}
}

func TestConfigureAndPingRejectsInvalidInputs(t *testing.T) {
	if err := Configure(nil, PoolConfig{}); !faults.IsCode(err, faults.CodeInvalidArgument) {
		t.Fatalf("Configure(nil) = %v", err)
	}
	if err := ConfigureAndPing(nil, nil, PoolConfig{}); !faults.IsCode(err, faults.CodeInvalidArgument) {
		t.Fatalf("ConfigureAndPing(nil) = %v", err)
	}
	if err := Ping(context.Background(), nil, 0); !faults.IsCode(err, faults.CodeInvalidArgument) {
		t.Fatalf("Ping(nil db) = %v", err)
	}
	if err := Ping(context.Background(), nil, -time.Second); !faults.IsCode(err, faults.CodeInvalidArgument) {
		t.Fatalf("Ping(invalid timeout) = %v", err)
	}
}
