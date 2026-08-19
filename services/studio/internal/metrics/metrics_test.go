// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.mindclade.dev/libs/go/observability"
)

func counter(name string, value float64) Collector {
	return func() observability.Measurement {
		return observability.Measurement{Name: name, Kind: observability.MetricCounter, Value: value}
	}
}

func scrape(t *testing.T, r *Registry) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	r.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	return rec.Code, rec.Body.String()
}

func TestScrapeRendersExpositionFormat(t *testing.T) {
	r := NewRegistry()
	if err := r.Register("studio.session.decrypt.failures", "failures seen", counter("studio.session.decrypt.failures", 3)); err != nil {
		t.Fatalf("Register: %v", err)
	}

	code, body := scrape(t, r)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	for _, want := range []string{
		"# HELP studio_session_decrypt_failures_total failures seen",
		"# TYPE studio_session_decrypt_failures_total counter",
		"studio_session_decrypt_failures_total 3",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("scrape missing %q; got:\n%s", want, body)
		}
	}
}

// The collector is read at scrape time, not at registration time. A counter
// captured once would report the value it had when the process started.
func TestScrapeReadsTheLiveValue(t *testing.T) {
	value := 0.0
	r := NewRegistry()
	if err := r.Register("studio.things", "", func() observability.Measurement {
		return observability.Measurement{Name: "studio.things", Kind: observability.MetricCounter, Value: value}
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	if _, body := scrape(t, r); !strings.Contains(body, "studio_things_total 0") {
		t.Fatalf("want 0 before increment; got:\n%s", body)
	}
	value = 7
	if _, body := scrape(t, r); !strings.Contains(body, "studio_things_total 7") {
		t.Fatalf("want 7 after increment; got:\n%s", body)
	}
}

// A bad collector must be refused at wiring time rather than serving a
// malformed scrape for the life of the deployment.
func TestRegisterRejectsInvalidCollectors(t *testing.T) {
	cases := map[string]Collector{
		"nil collector": nil,
		"invalid name":  counter("Studio.Bad Name", 1),
		"name mismatch": counter("studio.other", 1),
	}
	for name, collector := range cases {
		t.Run(name, func(t *testing.T) {
			if err := NewRegistry().Register("studio.expected", "", collector); err == nil {
				t.Errorf("Register(%s) succeeded; want an error", name)
			}
		})
	}
}

func TestRegisterRejectsDuplicates(t *testing.T) {
	r := NewRegistry()
	if err := r.Register("studio.things", "", counter("studio.things", 1)); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	if err := r.Register("studio.things", "", counter("studio.things", 1)); err == nil {
		t.Error("duplicate Register succeeded; want an error")
	}
}

// An up-down counter can decrease, so rate() over it is meaningless and
// Prometheus must be told it is a gauge.
func TestUpDownCounterIsExposedAsAGauge(t *testing.T) {
	r := NewRegistry()
	if err := r.Register("studio.active", "", func() observability.Measurement {
		return observability.Measurement{Name: "studio.active", Kind: observability.MetricUpDownCounter, Value: 2}
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	_, body := scrape(t, r)
	if !strings.Contains(body, "# TYPE studio_active gauge") {
		t.Errorf("want gauge type; got:\n%s", body)
	}
	if strings.Contains(body, "studio_active_total") {
		t.Errorf("only counters take the _total suffix; got:\n%s", body)
	}
}
