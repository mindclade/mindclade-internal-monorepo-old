// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package providers

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"strings"
	"time"

	"go.mindclade.dev/libs/go/auth"
	mcclock "go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/services/control_plane/internal/config"
)

// issuer names this process as the authority that minted the principal. It is
// recorded on every audit event, so it must stay stable across deployments.
const issuer = "control-plane/api-keys"

// apiKeyEntry is one configured service credential. Only the digest is held;
// the plaintext key never leaves the deployment secret.
type apiKeyEntry struct {
	subject     string
	digest      []byte
	permissions auth.PermissionSet
}

// apiKeyAuthenticator resolves service-to-service API keys against a registry
// supplied as deployment configuration.
//
// This is service identity, not user identity: it grants a fixed permission
// set to a named caller and has no session, refresh, or delegation semantics.
// User-facing authentication belongs to an identity provider and must not be
// grown out of this type.
//
// Principals do not expire, so revocation means removing the entry from the
// deployment secret and restarting the process. Rotate by adding the new
// subject, moving callers, then removing the old one.
type apiKeyAuthenticator struct {
	entries []apiKeyEntry
	clock   mcclock.Clock
}

func newAuthenticator(settings config.Settings, value mcclock.Clock) (auth.Authenticator, error) {
	entries, err := parseAPIKeys(settings.AuthAPIKeys)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, faults.New(
			faults.CodeFailedPrecondition,
			"control-plane API-key registry is not configured",
			faults.WithReason("api_keys_not_configured"),
			faults.WithOperation("controlplane.providers.newAuthenticator"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	return &apiKeyAuthenticator{entries: entries, clock: value}, nil
}

// Authenticate compares the presented secret against every configured digest
// without an early exit, so neither the number of configured keys nor the
// position of a match is observable in the response time.
func (authenticator *apiKeyAuthenticator) Authenticate(ctx context.Context, credential auth.Credential) (auth.Principal, error) {
	if ctx == nil || authenticator == nil {
		return auth.Principal{}, unauthenticated("authenticator_not_configured")
	}
	switch credential.Scheme() {
	case auth.CredentialSchemeAPIKey, auth.CredentialSchemeBearer:
	default:
		return auth.Principal{}, faults.New(
			faults.CodeUnauthenticated,
			"unsupported credential scheme",
			faults.WithReason("unsupported_credential_scheme"),
			faults.WithOperation("controlplane.providers.apiKeyAuthenticator.Authenticate"),
			faults.WithContextMetadata(ctx),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	presented := sha256.Sum256(credential.Value())
	matched := -1
	for index, entry := range authenticator.entries {
		if subtle.ConstantTimeCompare(entry.digest, presented[:]) == 1 {
			matched = index
		}
	}
	if matched < 0 {
		return auth.Principal{}, unauthenticated("api_key_not_recognized")
	}
	entry := authenticator.entries[matched]
	return auth.NewPrincipal(
		auth.PrincipalKindService,
		entry.subject,
		auth.WithIssuer(issuer),
		auth.WithPermissions(entry.permissions),
		auth.WithAuthenticationTimes(authenticator.clock.Now().UTC(), time.Time{}),
	)
}

// parseAPIKeys reads "subject:sha256hex:permission[,permission]" entries
// separated by ";". Duplicate subjects are rejected so a rotated credential
// cannot silently shadow the entry it was meant to replace.
func parseAPIKeys(raw string) ([]apiKeyEntry, error) {
	entries := make([]apiKeyEntry, 0)
	subjects := make(map[string]struct{})
	digests := make(map[string]struct{})
	for _, record := range strings.Split(raw, ";") {
		record = strings.TrimSpace(record)
		if record == "" {
			continue
		}
		fields := strings.Split(record, ":")
		if len(fields) != 3 {
			return nil, invalidAPIKey("malformed_api_key_entry", "")
		}
		subject := strings.TrimSpace(fields[0])
		if subject == "" {
			return nil, invalidAPIKey("empty_api_key_subject", "")
		}
		if _, exists := subjects[subject]; exists {
			return nil, invalidAPIKey("duplicate_api_key_subject", subject)
		}
		digest, err := hex.DecodeString(strings.TrimSpace(fields[1]))
		if err != nil || len(digest) != sha256.Size {
			return nil, invalidAPIKey("invalid_api_key_digest", subject)
		}
		// Two subjects sharing one secret would make the permission a caller
		// receives depend on entry order, and would make revoking one subject
		// silently leave the other working.
		fingerprint := string(digest)
		if _, exists := digests[fingerprint]; exists {
			return nil, invalidAPIKey("duplicate_api_key_digest", subject)
		}
		permissions, err := parsePermissions(fields[2])
		if err != nil {
			return nil, err
		}
		subjects[subject] = struct{}{}
		digests[fingerprint] = struct{}{}
		entries = append(entries, apiKeyEntry{subject: subject, digest: digest, permissions: permissions})
	}
	return entries, nil
}

func parsePermissions(raw string) (auth.PermissionSet, error) {
	values := make([]auth.Permission, 0)
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		permission, err := auth.ParsePermission(item)
		if err != nil {
			return auth.PermissionSet{}, err
		}
		values = append(values, permission)
	}
	if len(values) == 0 {
		return auth.PermissionSet{}, invalidAPIKey("empty_api_key_permissions", "")
	}
	return auth.NewPermissionSet(values...)
}

func unauthenticated(reason string) error {
	return faults.New(
		faults.CodeUnauthenticated,
		"authentication failed",
		faults.WithReason(reason),
		faults.WithOperation("controlplane.providers.apiKeyAuthenticator.Authenticate"),
		faults.WithRetryPolicy(faults.NoRetry()),
	)
}

func invalidAPIKey(reason, subject string) error {
	options := []faults.Option{
		faults.WithReason(reason),
		faults.WithOperation("controlplane.providers.parseAPIKeys"),
		faults.WithRetryPolicy(faults.NoRetry()),
	}
	if subject != "" {
		options = append(options, faults.WithField("subject", subject))
	}
	return faults.New(faults.CodeInvalidArgument, "invalid control-plane API-key registry", options...)
}
