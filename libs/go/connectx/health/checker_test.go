// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package health

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/grpchealth"

	"go.mindclade.dev/libs/go/servicekit"
)

type prober bool

func (value prober) Readiness(context.Context) servicekit.ProbeReport {
	return servicekit.ProbeReport{OK: bool(value), CheckedAt: time.Now()}
}

func TestChecker(t *testing.T) {
	checker, err := NewChecker(prober(true), "mindclade.test.v1.TestService")
	if err != nil {
		t.Fatal(err)
	}
	response, err := checker.Check(context.Background(), &grpchealth.CheckRequest{Service: "mindclade.test.v1.TestService"})
	if err != nil || response.Status != grpchealth.StatusServing {
		t.Fatalf("response=%#v err=%v", response, err)
	}
	if _, err := checker.Check(context.Background(), &grpchealth.CheckRequest{Service: "unknown"}); err == nil {
		t.Fatal("expected unknown service error")
	}
	unready, _ := NewChecker(prober(false))
	response, err = unready.Check(context.Background(), &grpchealth.CheckRequest{})
	if err != nil || response.Status != grpchealth.StatusNotServing {
		t.Fatalf("response=%#v err=%v", response, err)
	}
}

type pointerProber struct{}

func (*pointerProber) Readiness(context.Context) servicekit.ProbeReport {
	return servicekit.ProbeReport{OK: true}
}

func TestNewCheckerRejectsTypedNilProber(t *testing.T) {
	var value *pointerProber
	if _, err := NewChecker(value); err == nil {
		t.Fatal("expected typed-nil prober error")
	}
}

func TestCheckerRejectsNilContext(t *testing.T) {
	checker, err := NewChecker(prober(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := checker.Check(nil, nil); err == nil {
		t.Fatal("expected nil context error")
	}
}
