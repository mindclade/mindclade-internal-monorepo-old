// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.mindclade.dev/libs/go/observability"
	"go.mindclade.dev/services/studio/internal/metrics"
)

func testMetrics(t *testing.T) *metrics.Registry {
	t.Helper()
	r := metrics.NewRegistry()
	if err := r.Register("studio.session.decrypt.failures", "failures", func() observability.Measurement {
		return observability.Measurement{
			Name:  "studio.session.decrypt.failures",
			Kind:  observability.MetricCounter,
			Value: 4,
		}
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	return r
}

// A scraper carries no IAP assertion, so /metrics must sit outside the auth
// chain exactly as the probes do. Inside it, every scrape 401s and the counter
// is invisible for the life of the deployment.
func TestMetricsBypassAuthentication(t *testing.T) {
	d := Deps{
		Role: RoleWeb, Logger: discardLogger(), Health: testHealth(t), Metrics: testMetrics(t),
		Verifier: testVerifier(t), Codec: testCodec(t), Resolve: allowAll,
	}
	h, err := Build(d)
	if err != nil {
		t.Fatalf("Build(web): %v", err)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/metrics status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "studio_session_decrypt_failures_total 4") {
		t.Errorf("scrape did not carry the counter; got:\n%s", rec.Body.String())
	}
}

// The embed role is the cookieless surface, so it has no session counter to
// report — but it is still a process worth scraping.
func TestMetricsServedOnEmbed(t *testing.T) {
	h, err := Build(Deps{
		Role: RoleEmbed, Logger: discardLogger(), Health: testHealth(t), Metrics: testMetrics(t),
	})
	if err != nil {
		t.Fatalf("Build(embed): %v", err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("embed /metrics = %d, want 200", rec.Code)
	}
}

// A role wired without a registry must not serve a half-built endpoint.
func TestNoRegistryMeansNoMetricsRoute(t *testing.T) {
	h, err := Build(Deps{Role: RoleEmbed, Logger: discardLogger(), Health: testHealth(t)})
	if err != nil {
		t.Fatalf("Build(embed): %v", err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code == http.StatusOK {
		t.Errorf("/metrics served with no registry; status = %d", rec.Code)
	}
}
