// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package bootstrap

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
)

func TestDescribeProfileWorksWithoutProviderFactory(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := RunCommand(context.Background(), RoleScheduler, nil, []string{"--describe-profile"}, &stdout, &stderr)
	if code != ExitSuccess {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	var description commandDescription
	if err := json.Unmarshal(stdout.Bytes(), &description); err != nil {
		t.Fatal(err)
	}
	if description.Role != RoleScheduler.String() || description.Service == "" || len(description.Requirements) == 0 || len(description.Packages) == 0 {
		t.Fatalf("description=%+v", description)
	}
}

func TestUnconfiguredCommandFailsClosed(t *testing.T) {
	var stderr bytes.Buffer
	code := RunCommand(context.Background(), RoleAPI, UnconfiguredFactory("api"), nil, ioDiscard{}, &stderr)
	if code != ExitNotConfigured {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
}

// ioDiscard avoids importing io only for io.Discard and verifies the writer
// contract with a minimal implementation.
type ioDiscard struct{}

func (ioDiscard) Write(value []byte) (int, error) { return len(value), nil }
