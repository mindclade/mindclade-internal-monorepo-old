// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package health

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"mindclade.internal/libs/go/servicekit"
)

type prober struct{ ok bool }

func (value prober) Liveness(context.Context) servicekit.ProbeReport  { return value.report() }
func (value prober) Readiness(context.Context) servicekit.ProbeReport { return value.report() }
func (value prober) report() servicekit.ProbeReport {
	now := time.Now()
	return servicekit.ProbeReport{OK: value.ok, CheckedAt: now, Results: []servicekit.ProbeResult{{Name: "component", OK: value.ok, CheckedAt: now}}}
}

func TestHandlerStatus(t *testing.T) {
	for _, test := range []struct {
		ok     bool
		status int
	}{{true, http.StatusOK}, {false, http.StatusServiceUnavailable}} {
		recorder := httptest.NewRecorder()
		NewHandler(prober{ok: test.ok}, Config{}).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/readyz", nil))
		if recorder.Code != test.status {
			t.Fatalf("ok=%v status=%d", test.ok, recorder.Code)
		}
	}
}
