// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package apikeys

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"go.mindclade.dev/libs/go/auth"
	mcclock "go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/services/control_plane/internal/config"
)

const testAPIKey = "registry-client-secret-value"

func testSettings(t *testing.T) config.Settings {
	t.Helper()
	digest := sha256.Sum256([]byte(testAPIKey))
	return config.Settings{
		AuthAPIKeys: "registry-client:" + hex.EncodeToString(digest[:]) + ":artifacts.read",
	}
}

func TestAPIKeyAuthenticatorResolvesAndRejects(t *testing.T) {
	authenticator, err := NewAuthenticator(testSettings(t), mcclock.RealClock{})
	if err != nil {
		t.Fatal(err)
	}
	credential, err := auth.APIKey(testAPIKey)
	if err != nil {
		t.Fatal(err)
	}
	principal, err := auth.Authenticate(context.Background(), authenticator, credential)
	if err != nil {
		t.Fatal(err)
	}
	if principal.Subject() != "registry-client" || principal.Kind() != auth.PrincipalKindService {
		t.Fatalf("principal=%+v", principal)
	}
	if !principal.Allows(auth.MustParsePermission("artifacts.read")) {
		t.Fatal("expected granted permission")
	}
	if principal.Allows(auth.MustParsePermission("artifacts.delete")) {
		t.Fatal("unexpected permission grant")
	}

	wrong, err := auth.APIKey("not-the-configured-secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := auth.Authenticate(context.Background(), authenticator, wrong); err == nil {
		t.Fatal("authenticator accepted an unknown key")
	}
}

// Two subjects sharing one secret would make the permission a caller receives
// depend on entry order, and would make revoking one subject silently leave
// the other working.
func TestAPIKeyRegistryRejectsSharedSecrets(t *testing.T) {
	digest := hex.EncodeToString(func() []byte { sum := sha256.Sum256([]byte("shared")); return sum[:] }())
	_, err := parseAPIKeys("first:" + digest + ":runs.read;second:" + digest + ":runs.write")
	if err == nil || faults.ReasonOf(err) != "duplicate_api_key_digest" {
		t.Fatalf("err=%v", err)
	}
}

// A rotated credential must not silently shadow the entry it replaces.
func TestAPIKeyRegistryRejectsDuplicateSubjects(t *testing.T) {
	digest := hex.EncodeToString(func() []byte { sum := sha256.Sum256([]byte("a")); return sum[:] }())
	_, err := parseAPIKeys("client:" + digest + ":runs.read;client:" + digest + ":runs.write")
	if err == nil || faults.ReasonOf(err) != "duplicate_api_key_subject" {
		t.Fatalf("err=%v", err)
	}
}

// An empty registry is a deployment error, not an open door.
func TestUnconfiguredRegistryFailsClosed(t *testing.T) {
	if _, err := NewAuthenticator(config.Settings{}, mcclock.RealClock{}); err == nil {
		t.Fatal("empty API-key registry was accepted")
	} else if reason := faults.ReasonOf(err); reason != "api_keys_not_configured" {
		t.Fatalf("reason=%s", reason)
	}
}
