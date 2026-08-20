// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package main

import (
	"context"
	"fmt"
	"strconv"

	"go.mindclade.dev/libs/go/config"
	"go.mindclade.dev/services/studio/internal/server"
)

// Logical configuration keys.
//
// These are the estate's names for the settings; the environment variables that
// carry them are an input mapping, declared once in envMapping below. The
// indirection is what lets a second source — a mounted file, a secret manager —
// be added later without every read site changing.
const (
	keyRole                 = "studio.role"
	keyListenAddress        = "listen.address"
	keyCSPEnforce           = "csp.enforce"
	keyCSPReportURI         = "csp.report.uri"
	keyIAPAudience          = "iap.audience"
	keyAuthorizedSubjects   = "authz.authorized.subjects"
	keySessionKeyCurrentID  = "session.key.current.id"
	keySessionKeyCurrent    = "session.key.current"
	keySessionKeyPreviousID = "session.key.previous.id"
	keySessionKeyPrevious   = "session.key.previous"
	keyAuthzVersion         = "authz.version"
	keyDatabaseURL          = "database.url"
)

// envMapping is the ONLY place an environment variable name appears.
//
// config.EnvSource reads exactly these variables and never scans the
// environment, so an unrelated variable cannot be captured into the snapshot
// and an unknown key fails the load rather than being ignored.
var envMapping = map[string]string{
	keyRole:                 "STUDIO_ROLE",
	keyListenAddress:        "LISTEN_ADDRESS",
	keyCSPEnforce:           "CSP_ENFORCE",
	keyCSPReportURI:         "CSP_REPORT_URI",
	keyIAPAudience:          "IAP_AUDIENCE",
	keyAuthorizedSubjects:   "AUTHORIZED_IAP_SUBJECTS",
	keySessionKeyCurrentID:  "SESSION_KEY_CURRENT_ID",
	keySessionKeyCurrent:    "SESSION_KEY_CURRENT",
	keySessionKeyPreviousID: "SESSION_KEY_PREVIOUS_ID",
	keySessionKeyPrevious:   "SESSION_KEY_PREVIOUS",
	keyAuthzVersion:         "AUTHZ_VERSION",
	keyDatabaseURL:          "DATABASE_URL",
}

// envName reports the environment variable a key is read from, so an error
// still names what an operator has to set.
func envName(key string) string {
	if name, ok := envMapping[key]; ok {
		return name
	}
	return key
}

// value returns a key's value, or "" when it was never supplied.
//
// Only for keys that are optional at the schema level. A key with a Default or
// with Required set is always present, and those use MustGet so a schema and a
// read site that disagree panic at startup rather than silently reading "".
func value(settings config.Snapshot, key string) string {
	v, _ := settings.Get(key)
	return v
}

// settingsSchema declares every setting studio reads.
//
// # Why several settings are optional here and required later
//
// Required is a property of the schema, but studio's four roles need different
// subsets: embed runs with no IAP audience, no session keys, and no database
// BY DESIGN. Declaring those Required would make the schema demand credentials
// of the one role that must not have them. So the schema declares the surface
// and each role's construction path enforces its own subset, with the
// fail-closed error message that names the variable.
func settingsSchema() []config.Field {
	return []config.Field{
		{
			Key:      keyRole,
			Required: true,
			Validate: func(v string) error {
				_, err := server.ParseRole(v)
				return err
			},
		},
		{
			Key:     keyListenAddress,
			Default: config.String(":8080"),
		},
		{
			// Report-Only unless explicitly enforced, so the default is the
			// safe direction. Validated as a strict boolean: "TRUE" or "1"
			// silently meaning Report-Only is the kind of typo that is
			// discovered by a CSP that was never enforcing.
			Key:      keyCSPEnforce,
			Default:  config.String("false"),
			Validate: boolean,
		},
		{Key: keyCSPReportURI},
		{Key: keyIAPAudience},
		// The value is not a credential, but it is an access-control list of
		// stable identity identifiers. Redact it from the configuration log.
		{Key: keyAuthorizedSubjects, Secret: true},
		{Key: keySessionKeyCurrentID},
		{Key: keySessionKeyCurrent, Secret: true},
		{Key: keySessionKeyPreviousID},
		{Key: keySessionKeyPrevious, Secret: true},
		{
			Key:      keyAuthzVersion,
			Default:  config.String("1"),
			Validate: integer,
		},
		// Secret because a DSN carries the password. Marking it is what makes
		// logging the whole redacted snapshot at startup safe.
		{Key: keyDatabaseURL, Secret: true},
	}
}

func loadSettings(ctx context.Context) (config.Snapshot, error) {
	loader, err := config.New(settingsSchema(), config.EnvSource{Mapping: envMapping})
	if err != nil {
		return config.Snapshot{}, err
	}
	return loader.Load(ctx)
}

func boolean(v string) error {
	if v != "true" && v != "false" {
		return fmt.Errorf("must be exactly %q or %q, got %q", "true", "false", v)
	}
	return nil
}

func integer(v string) error {
	if _, err := strconv.Atoi(v); err != nil {
		return fmt.Errorf("must be an integer: %w", err)
	}
	return nil
}
