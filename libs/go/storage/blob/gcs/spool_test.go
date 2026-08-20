// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package gcs

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
)

func TestCreateSpool(t *testing.T) {
	payload := []byte("mindclade")
	staged, err := createSpool(context.Background(), bytes.NewReader(payload), t.TempDir(), int64(len(payload)))
	if err != nil {
		t.Fatal(err)
	}
	name := staged.file.Name()
	defer staged.Close()
	if staged.size != int64(len(payload)) || !staged.digest.Equal(identifiers.SHA256(payload)) || name == "" {
		t.Fatalf("spool = %#v", staged)
	}
}

func TestCreateSpoolLimitAndCancellation(t *testing.T) {
	_, err := createSpool(context.Background(), bytes.NewReader([]byte("too large")), t.TempDir(), 2)
	if faults.CodeOf(err) != faults.CodeResourceExhausted {
		t.Fatalf("limit error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = createSpool(ctx, bytes.NewReader([]byte("value")), t.TempDir(), 100)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
}

func TestCreateSpoolQualifiesCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := createSpool(ctx, strings.NewReader("payload"), t.TempDir(), 1024)
	if !faults.IsCode(err, faults.CodeCanceled) || !errors.Is(err, context.Canceled) {
		t.Fatalf("createSpool() = %v", err)
	}
}
