// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package main

import (
	"context"
	"errors"
	"testing"

	"go.mindclade.dev/libs/go/config"
	"go.mindclade.dev/services/studio/internal/httpx"
	"go.mindclade.dev/services/studio/internal/server"
)

// Every logical key must be readable, or a read site names a key the schema
// does not define and MustGet panics in production rather than in this test.
func TestEveryDeclaredKeyIsMapped(t *testing.T) {
	for _, field := range settingsSchema() {
		if _, ok := envMapping[field.Key]; !ok {
			t.Errorf("field %q has no environment mapping", field.Key)
		}
	}
	for key := range envMapping {
		found := false
		for _, field := range settingsSchema() {
			if field.Key == key {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("mapping %q has no schema field; EnvSource would fail the load as an unknown key", key)
		}
	}
}

func TestDefaultsApplyWhenUnset(t *testing.T) {
	t.Setenv("STUDIO_ROLE", "embed")

	settings, err := loadSettings(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := settings.MustGet(keyListenAddress); got != ":8080" {
		t.Errorf("listen address = %q, want :8080", got)
	}
	// Report-Only is the safe direction, so it must be what an unset
	// environment produces.
	if mode := cspMode(settings); mode != httpx.CSPReportOnly {
		t.Errorf("csp mode = %v, want report-only", mode)
	}
}

// Secrets must be redacted, because startup logs the whole snapshot.
func TestSecretsAreRedacted(t *testing.T) {
	t.Setenv("STUDIO_ROLE", "bff")
	t.Setenv("DATABASE_URL", "postgres://user:hunter2@host/db")
	t.Setenv("SESSION_KEY_CURRENT", "c2VjcmV0LW1hdGVyaWFs")

	settings, err := loadSettings(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	redacted := settings.Redacted()
	for _, key := range []string{keyDatabaseURL, keySessionKeyCurrent} {
		if redacted[key] != "[REDACTED]" {
			t.Errorf("%s = %q, want [REDACTED]", key, redacted[key])
		}
	}
	// Redaction must not be achieved by dropping the value.
	if settings.MustGet(keyDatabaseURL) != "postgres://user:hunter2@host/db" {
		t.Error("redaction changed the value the process reads")
	}
}

func TestRoleIsRequiredAndValidated(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		t.Setenv("STUDIO_ROLE", "")
		if _, err := loadSettings(context.Background()); !errors.Is(err, config.ErrRequiredMissing) {
			t.Errorf("err = %v, want ErrRequiredMissing", err)
		}
	})

	t.Run("unknown", func(t *testing.T) {
		t.Setenv("STUDIO_ROLE", "backend")
		_, err := loadSettings(context.Background())
		if !errors.Is(err, config.ErrInvalidValue) {
			t.Errorf("err = %v, want ErrInvalidValue", err)
		}
	})

	t.Run("every real role loads", func(t *testing.T) {
		for _, role := range []server.Role{server.RoleWeb, server.RoleBFF, server.RoleBFFStream, server.RoleEmbed} {
			t.Setenv("STUDIO_ROLE", string(role))
			if _, err := loadSettings(context.Background()); err != nil {
				t.Errorf("role %q: %v", role, err)
			}
		}
	})
}

// A near-miss boolean must fail loudly. "TRUE" silently meaning Report-Only is
// how a CSP rollout is discovered never to have been enforcing.
func TestCSPEnforceRejectsNearMissBooleans(t *testing.T) {
	for _, value := range []string{"TRUE", "1", "yes", "True"} {
		t.Setenv("STUDIO_ROLE", "web")
		t.Setenv("CSP_ENFORCE", value)
		if _, err := loadSettings(context.Background()); err == nil {
			t.Errorf("CSP_ENFORCE=%q was accepted", value)
		}
	}

	t.Setenv("STUDIO_ROLE", "web")
	t.Setenv("CSP_ENFORCE", "true")
	settings, err := loadSettings(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cspMode(settings) == httpx.CSPReportOnly {
		t.Error("CSP_ENFORCE=true did not enforce")
	}
}

func TestAuthzVersionMustBeAnInteger(t *testing.T) {
	t.Setenv("STUDIO_ROLE", "bff")
	t.Setenv("AUTHZ_VERSION", "one")
	if _, err := loadSettings(context.Background()); err == nil {
		t.Error("AUTHZ_VERSION=one was accepted")
	}
}

// The digest identifies a configuration across pods, so equal inputs must give
// equal digests and any change must move it.
func TestDigestIsStableAndSensitive(t *testing.T) {
	t.Setenv("STUDIO_ROLE", "web")

	first, err := loadSettings(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	second, err := loadSettings(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !first.Equal(second) {
		t.Error("identical configuration produced different digests")
	}

	t.Setenv("LISTEN_ADDRESS", ":9090")
	changed, err := loadSettings(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if changed.Equal(first) {
		t.Error("a changed listen address did not move the digest")
	}
}
