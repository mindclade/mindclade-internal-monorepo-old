// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package grpcx

import (
	"google.golang.org/grpc/codes"
	"mindclade.internal/libs/go/faults"
	"testing"
)

func TestCodeMappings(t *testing.T) {
	cases := []struct {
		fault faults.Code
		grpc  codes.Code
	}{{faults.CodeNotFound, codes.NotFound}, {faults.CodeUnavailable, codes.Unavailable}, {faults.CodeUnauthenticated, codes.Unauthenticated}}
	for _, test := range cases {
		if got := CodeFromFault(test.fault); got != test.grpc {
			t.Errorf("%s => %v", test.fault, got)
		}
		if got := FaultCode(test.grpc); got != test.fault {
			t.Errorf("%v => %s", test.grpc, got)
		}
	}
}
