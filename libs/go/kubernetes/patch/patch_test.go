// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package patch

import (
	"context"
	"errors"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"

	"go.mindclade.dev/libs/go/faults"
)

type fakeApplyConfiguration struct{}

func (*fakeApplyConfiguration) IsApplyConfiguration() {}

type recordingSubresource struct {
	crclient.SubResourceWriter
	patched  crclient.Object
	applied  runtime.ApplyConfiguration
	patchErr error
	applyErr error
}

func (writer *recordingSubresource) Patch(_ context.Context, object crclient.Object, _ crclient.Patch, _ ...crclient.SubResourcePatchOption) error {
	writer.patched = object
	return writer.patchErr
}

func (writer *recordingSubresource) Apply(_ context.Context, configuration runtime.ApplyConfiguration, _ ...crclient.SubResourceApplyOption) error {
	writer.applied = configuration
	return writer.applyErr
}

type recordingClient struct {
	crclient.Client
	patched  crclient.Object
	applied  runtime.ApplyConfiguration
	patchErr error
	applyErr error
	status   *recordingSubresource
}

func (client *recordingClient) Patch(_ context.Context, object crclient.Object, _ crclient.Patch, _ ...crclient.PatchOption) error {
	client.patched = object
	return client.patchErr
}

func (client *recordingClient) Apply(_ context.Context, configuration runtime.ApplyConfiguration, _ ...crclient.ApplyOption) error {
	client.applied = configuration
	return client.applyErr
}

func (client *recordingClient) Status() crclient.SubResourceWriter {
	return client.status
}

func snapshots() (*metav1.PartialObjectMetadata, *metav1.PartialObjectMetadata) {
	before := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Name: "example", Namespace: "default", ResourceVersion: "7"}}
	after := before.DeepCopy()
	after.SetAnnotations(map[string]string{"mindclade.dev/version": "2"})
	return before, after
}

func TestObjectAndStatusPatch(t *testing.T) {
	before, after := snapshots()
	client := &recordingClient{status: &recordingSubresource{}}
	if err := Object(context.Background(), client, before, after); err != nil {
		t.Fatal(err)
	}
	if client.patched != after {
		t.Fatal("Object did not patch the mutated snapshot")
	}
	if err := Status(context.Background(), client, before, after); err != nil {
		t.Fatal(err)
	}
	if client.status.patched != after {
		t.Fatal("Status did not patch the mutated snapshot")
	}
}

func TestObjectQualifiesConflict(t *testing.T) {
	before, after := snapshots()
	provider := &apierrors.StatusError{ErrStatus: metav1.Status{Reason: metav1.StatusReasonConflict, Code: 409}}
	client := &recordingClient{patchErr: provider}
	err := Object(context.Background(), client, before, after)
	if !faults.IsCode(err, faults.CodeConflict) || !faults.IsRetryable(err) || !errors.Is(err, provider) {
		t.Fatalf("Object() = %v", err)
	}
}

func TestApplyAndApplyStatus(t *testing.T) {
	configuration := &fakeApplyConfiguration{}
	client := &recordingClient{status: &recordingSubresource{}}
	if err := Apply(context.Background(), client, configuration, "mindclade-controller", true); err != nil {
		t.Fatal(err)
	}
	if client.applied != configuration {
		t.Fatal("Apply did not forward configuration")
	}
	if err := ApplyStatus(context.Background(), client, configuration, "mindclade-controller", false); err != nil {
		t.Fatal(err)
	}
	if client.status.applied != configuration {
		t.Fatal("ApplyStatus did not forward configuration")
	}
}

func TestValidation(t *testing.T) {
	before, after := snapshots()
	client := &recordingClient{status: &recordingSubresource{}}

	missingVersion := before.DeepCopy()
	missingVersion.SetResourceVersion("")
	if err := Object(context.Background(), client, missingVersion, after); !faults.IsCode(err, faults.CodeFailedPrecondition) {
		t.Fatalf("missing resource version = %v", err)
	}

	other := after.DeepCopy()
	other.SetName("other")
	if err := Object(context.Background(), client, before, other); !faults.IsCode(err, faults.CodeInvalidArgument) {
		t.Fatalf("identity mismatch = %v", err)
	}

	if err := Object(nil, client, before, after); !faults.IsCode(err, faults.CodeInvalidArgument) {
		t.Fatalf("nil context = %v", err)
	}
	if err := Apply(context.Background(), client, &fakeApplyConfiguration{}, " ", false); !faults.IsCode(err, faults.CodeInvalidArgument) {
		t.Fatalf("empty field owner = %v", err)
	}
	if err := ApplyStatus(context.Background(), nil, &fakeApplyConfiguration{}, "owner", false); !faults.IsCode(err, faults.CodeInvalidArgument) {
		t.Fatalf("nil client = %v", err)
	}
}
