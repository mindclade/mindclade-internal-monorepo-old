// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package config

import (
	"strings"

	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/signing"
)

// Validate applies cross-field rules that cannot be expressed by individual
// schema validators.
func (settings Settings) Validate() error {
	if !settings.Environment.Valid() || strings.TrimSpace(settings.ServiceName) == "" {
		return invalid("invalid_settings_identity", "controlplane.config.Settings.Validate", nil)
	}
	if settings.DatabaseMaxOpen > 0 && settings.DatabaseMaxIdle > settings.DatabaseMaxOpen {
		return invalid("database_idle_exceeds_open", "controlplane.config.Settings.Validate", nil)
	}
	if settings.DrainTimeout > settings.ShutdownTimeout {
		return invalid("drain_exceeds_shutdown", "controlplane.config.Settings.Validate", nil)
	}
	if _, err := signing.ParseKeyID(settings.SigningKeyID); err != nil {
		return err
	}
	if settings.PaginationTTL <= 0 {
		return invalid("invalid_pagination_ttl", "controlplane.config.Settings.Validate", nil)
	}
	if settings.Environment == EnvironmentStaging || settings.Environment == EnvironmentProduction {
		if strings.TrimSpace(settings.DatabaseDSN) == "" {
			return invalid("durable_database_required", "controlplane.config.Settings.Validate", nil)
		}
		if err := requireDatabaseTLS(settings.DatabaseDSN); err != nil {
			return err
		}
		if len(settings.SigningHMACKey) < signing.MinimumHMACKeySize {
			return invalid("production_signing_key_required", "controlplane.config.Settings.Validate", nil)
		}
		if settings.MessagingProvider == "memory" {
			return invalid("durable_messaging_required", "controlplane.config.Settings.Validate", nil)
		}
	}
	return nil
}

// insecureSSLModes are the libpq sslmode values that either send credentials
// and rows in the clear or accept any certificate presented to them. "prefer"
// is included because it silently downgrades: it tries TLS and continues
// without it, so a misconfigured server turns into a plaintext session with no
// error anywhere.
var insecureSSLModes = map[string]struct{}{
	"disable": {}, "allow": {}, "prefer": {},
}

// requireDatabaseTLS refuses a staging or production DSN that would not
// encrypt. libs/go/storage/sql/postgres has no TLS surface of its own -- the
// driver reads it from the DSN -- so this is the only place the deployment can
// be held to it.
//
// An absent sslmode is refused rather than assumed. libpq defaults to
// "prefer", which is one of the downgrading modes, so silence is not consent.
func requireDatabaseTLS(dsn string) error {
	mode, found := sslModeOf(dsn)
	if !found {
		return invalid("database_sslmode_unset", "controlplane.config.requireDatabaseTLS", faults.Fields{
			"expected": "sslmode=require, verify-ca, or verify-full",
		})
	}
	if _, insecure := insecureSSLModes[mode]; insecure {
		return invalid("database_sslmode_insecure", "controlplane.config.requireDatabaseTLS", faults.Fields{
			"sslmode":  mode,
			"expected": "sslmode=require, verify-ca, or verify-full",
		})
	}
	return nil
}

// sslModeOf reads sslmode from either DSN form the driver accepts: a URL
// (postgres://host/db?sslmode=require) or keyword=value pairs. Only the key is
// parsed; the driver owns interpreting the rest.
func sslModeOf(dsn string) (string, bool) {
	dsn = strings.TrimSpace(dsn)
	if index := strings.Index(dsn, "?"); index >= 0 {
		dsn = dsn[index+1:]
	}
	for _, field := range strings.FieldsFunc(dsn, func(value rune) bool {
		return value == ' ' || value == '&'
	}) {
		key, value, ok := strings.Cut(field, "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(key), "sslmode") {
			continue
		}
		return strings.ToLower(strings.Trim(strings.TrimSpace(value), `'"`)), true
	}
	return "", false
}

func invalid(reason, operation string, fields faults.Fields) error {
	return faults.New(
		faults.CodeInvalidArgument,
		"invalid control-plane configuration",
		faults.WithReason(reason),
		faults.WithOperation(operation),
		faults.WithFields(fields),
		faults.WithRetryPolicy(faults.NoRetry()),
	)
}
