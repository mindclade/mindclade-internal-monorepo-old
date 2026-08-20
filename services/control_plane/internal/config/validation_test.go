// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package config

import (
	"context"
	"testing"

	foundationconfig "go.mindclade.dev/libs/go/config"
	"go.mindclade.dev/libs/go/faults"
)

// production is the smallest source that reaches the staging/production
// branch of Validate, so a case can vary the DSN and nothing else.
func production(dsn string) foundationconfig.MapSource {
	return foundationconfig.MapSource{SourceName: "test", Values: map[string]string{
		"environment":        "production",
		"database.dsn":       dsn,
		"signing.hmac_key":   "01234567890123456789012345678901",
		"messaging.provider": "pubsub",
	}}
}

// libs/go/storage/sql/postgres has no TLS surface: the driver reads sslmode
// from the DSN, so configuration is the only place a deployment can be held to
// encrypting. Every downgrading mode has to be refused by name.
func TestProductionRefusesInsecureSSLModes(t *testing.T) {
	for _, mode := range []string{"disable", "allow", "prefer"} {
		t.Run(mode, func(t *testing.T) {
			_, err := Load(context.Background(), "control-plane-api",
				production("postgres://control:secret@db:5432/control?sslmode="+mode))
			if err == nil || faults.ReasonOf(err) != "database_sslmode_insecure" {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

// Silence is not consent. libpq defaults to "prefer", which downgrades to
// plaintext without reporting anything, so an absent sslmode must fail rather
// than inherit the driver default.
func TestProductionRefusesAnUnsetSSLMode(t *testing.T) {
	_, err := Load(context.Background(), "control-plane-api",
		production("postgres://control:secret@db:5432/control"))
	if err == nil || faults.ReasonOf(err) != "database_sslmode_unset" {
		t.Fatalf("err=%v", err)
	}
}

func TestProductionAcceptsVerifyingSSLModes(t *testing.T) {
	for _, mode := range []string{"require", "verify-ca", "verify-full"} {
		t.Run(mode, func(t *testing.T) {
			if _, err := Load(context.Background(), "control-plane-api",
				production("postgres://control:secret@db:5432/control?sslmode="+mode)); err != nil {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

// Both DSN forms the driver accepts have to be read, or the rule is trivially
// bypassed by writing the other one.
func TestKeywordValueDSNIsRead(t *testing.T) {
	_, err := Load(context.Background(), "control-plane-api",
		production("host=db port=5432 user=control dbname=control sslmode=disable"))
	if err == nil || faults.ReasonOf(err) != "database_sslmode_insecure" {
		t.Fatalf("err=%v", err)
	}
	if _, err := Load(context.Background(), "control-plane-api",
		production("host=db port=5432 user=control dbname=control sslmode=verify-full")); err != nil {
		t.Fatalf("err=%v", err)
	}
}

// Development is where a local plaintext database is the point. The rule must
// not reach it, or nobody can run the service on a laptop.
func TestDevelopmentAllowsPlaintext(t *testing.T) {
	_, err := Load(context.Background(), "control-plane-api", foundationconfig.MapSource{
		SourceName: "test",
		Values:     map[string]string{"database.dsn": "postgres://localhost:5432/control?sslmode=disable"},
	})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
}

func TestSSLModeParsingIsCaseAndQuoteInsensitive(t *testing.T) {
	for _, dsn := range []string{
		"postgres://db/control?SSLMode=DISABLE",
		"host=db sslmode='disable'",
	} {
		if _, err := Load(context.Background(), "control-plane-api", production(dsn)); err == nil ||
			faults.ReasonOf(err) != "database_sslmode_insecure" {
			t.Fatalf("dsn=%q err=%v", dsn, err)
		}
	}
}
