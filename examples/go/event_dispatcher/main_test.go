// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func TestRunPublishesAndCommitsOutboxRecord(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var output bytes.Buffer
	if err := run(ctx, &output); err != nil {
		t.Fatalf("run: %v", err)
	}
	text := output.String()
	for _, expected := range []string{"delivered topic=runs.created", "outbox state=published"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("output %q does not contain %q", text, expected)
		}
	}
}
