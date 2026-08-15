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
		if len(settings.SigningHMACKey) < signing.MinimumHMACKeySize {
			return invalid("production_signing_key_required", "controlplane.config.Settings.Validate", nil)
		}
		if settings.MessagingProvider == "memory" {
			return invalid("durable_messaging_required", "controlplane.config.Settings.Validate", nil)
		}
	}
	return nil
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
