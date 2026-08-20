// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package server

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"

	_ "github.com/lib/pq"
)

// probe drives one probe path against a role's real handler.
func probe(t *testing.T, h http.Handler, path string) int {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec.Code
}

// The defect this whole change exists for: /readyz used to be wired to a
// handler that wrote 200 unconditionally, so a pod stayed in the Endpoints
// list all the way through its own shutdown. Draining must take it out.
func TestReadinessFailsOnceDraining(t *testing.T) {
	for _, role := range []Role{RoleWeb, RoleEmbed} {
		d := Deps{Role: role, Logger: discardLogger(), Health: testHealth(t)}
		if role != RoleEmbed {
			d.Verifier = testVerifier(t)
			d.Codec = testCodec(t)
			d.Resolve = allowAll
		}
		h, err := Build(d)
		if err != nil {
			t.Fatalf("Build(%s): %v", role, err)
		}

		if got := probe(t, h, "/readyz"); got != http.StatusOK {
			t.Errorf("%s /readyz before drain = %d, want 200", role, got)
		}

		d.Health.BeginDrain()

		if got := probe(t, h, "/readyz"); got != http.StatusServiceUnavailable {
			t.Errorf("%s /readyz while draining = %d, want 503", role, got)
		}
		// Liveness must NOT follow readiness down. A failed liveness probe makes
		// the kubelet kill the container, which would cut the drain short.
		if got := probe(t, h, "/healthz"); got != http.StatusOK {
			t.Errorf("%s /healthz while draining = %d, want 200", role, got)
		}
	}
}

// An unreachable database must take the pod out of rotation without making the
// kubelet kill it: readiness fails, liveness holds. The inverse — liveness
// depending on the database — turns one database outage into an estate-wide
// crash loop.
func TestUnreachableDatabaseFailsReadinessOnly(t *testing.T) {
	// Port 1 refuses immediately; sql.Open does not dial, so this is only
	// reached by the readiness probe's Ping.
	db, err := sql.Open("postgres", "postgres://user@127.0.0.1:1/studio?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	health, err := NewHealth(db)
	if err != nil {
		t.Fatalf("NewHealth: %v", err)
	}
	h, err := Build(Deps{
		Role: RoleBFF, Logger: discardLogger(), Health: health,
		Verifier: testVerifier(t), Codec: testCodec(t), Resolve: allowAll,
		DB: db,
	})
	if err != nil {
		t.Fatalf("Build(bff): %v", err)
	}

	if got := probe(t, h, "/readyz"); got != http.StatusServiceUnavailable {
		t.Errorf("/readyz with an unreachable database = %d, want 503", got)
	}
	if got := probe(t, h, "/healthz"); got != http.StatusOK {
		t.Errorf("/healthz with an unreachable database = %d, want 200", got)
	}
}

// The embed role has no database and must still be ready. Its readiness set is
// empty by design, not by omission.
func TestEmbedIsReadyWithNoDatabase(t *testing.T) {
	h, err := Build(Deps{Role: RoleEmbed, Logger: discardLogger(), Health: testHealth(t)})
	if err != nil {
		t.Fatalf("Build(embed): %v", err)
	}
	if got := probe(t, h, "/readyz"); got != http.StatusOK {
		t.Errorf("embed /readyz = %d, want 200", got)
	}
}
