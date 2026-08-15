// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package gcs

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"google.golang.org/api/googleapi"

	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/storage/blob"
)

func TestQualifyProviderError(t *testing.T) {
	key := blob.MustParseKey("object")
	tests := []struct {
		name   string
		code   int
		intent errorIntent
		want   faults.Code
	}{
		{name: "not_found", code: http.StatusNotFound, want: faults.CodeNotFound},
		{name: "create_conflict", code: http.StatusPreconditionFailed, intent: intentCreateOnly, want: faults.CodeAlreadyExists},
		{name: "generation_conflict", code: http.StatusPreconditionFailed, intent: intentGenerationMatch, want: faults.CodeConflict},
		{name: "throttled", code: http.StatusTooManyRequests, want: faults.CodeResourceExhausted},
		{name: "unavailable", code: http.StatusServiceUnavailable, want: faults.CodeUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := &googleapi.Error{Code: test.code, Message: "provider detail"}
			err := qualify(context.Background(), provider, "test", "bucket", key, test.intent)
			if faults.CodeOf(err) != test.want || !errors.Is(err, provider) {
				t.Fatalf("qualify() = %v", err)
			}
			if faults.PublicMessageOf(err) == provider.Error() {
				t.Fatal("provider detail exposed as public message")
			}
		})
	}
}
