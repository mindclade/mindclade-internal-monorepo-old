// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package patch

import (
	"context"
	"reflect"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"

	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/kubernetes"
)

// Object persists the difference between before and after as an optimistic
// merge patch. before must be a deep copy captured before after was mutated.
func Object(ctx context.Context, client crclient.Client, before, after crclient.Object, options ...crclient.PatchOption) error {
	const operation = "kubernetes.patch.Object"
	if err := validateObjects(ctx, client, before, after, operation); err != nil {
		return err
	}
	patch := crclient.MergeFromWithOptions(before, crclient.MergeFromWithOptimisticLock{})
	if err := client.Patch(ctx, after, patch, options...); err != nil {
		return qualify(ctx, err, operation, after)
	}
	return nil
}

// Status persists the status-subresource difference between before and after
// as an optimistic merge patch.
func Status(ctx context.Context, client crclient.Client, before, after crclient.Object, options ...crclient.SubResourcePatchOption) error {
	const operation = "kubernetes.patch.Status"
	if err := validateObjects(ctx, client, before, after, operation); err != nil {
		return err
	}
	patch := crclient.MergeFromWithOptions(before, crclient.MergeFromWithOptimisticLock{})
	if err := client.Status().Patch(ctx, after, patch, options...); err != nil {
		return qualify(ctx, err, operation, after)
	}
	return nil
}

// Apply performs server-side apply using controller-runtime's apply-
// configuration API. Field ownership must be explicit and stable across
// releases. Force should be enabled only for fields Mindclade exclusively owns.
func Apply(ctx context.Context, client crclient.Client, configuration runtime.ApplyConfiguration, fieldOwner string, force bool, options ...crclient.ApplyOption) error {
	const operation = "kubernetes.patch.Apply"
	if ctx == nil || nilInterface(client) || nilInterface(configuration) || strings.TrimSpace(fieldOwner) == "" {
		return invalid(operation)
	}
	options = append(options, crclient.FieldOwner(strings.TrimSpace(fieldOwner)))
	if force {
		options = append(options, crclient.ForceOwnership)
	}
	if err := client.Apply(ctx, configuration, options...); err != nil {
		return kubernetes.Qualify(ctx, err, operation, faults.Fields{"field_owner": strings.TrimSpace(fieldOwner)})
	}
	return nil
}

// ApplyStatus performs server-side apply against the status subresource.
func ApplyStatus(ctx context.Context, client crclient.Client, configuration runtime.ApplyConfiguration, fieldOwner string, force bool, options ...crclient.SubResourceApplyOption) error {
	const operation = "kubernetes.patch.ApplyStatus"
	if ctx == nil || nilInterface(client) || nilInterface(configuration) || strings.TrimSpace(fieldOwner) == "" {
		return invalid(operation)
	}
	options = append(options, crclient.FieldOwner(strings.TrimSpace(fieldOwner)))
	if force {
		options = append(options, crclient.ForceOwnership)
	}
	if err := client.Status().Apply(ctx, configuration, options...); err != nil {
		return kubernetes.Qualify(ctx, err, operation, faults.Fields{"field_owner": strings.TrimSpace(fieldOwner)})
	}
	return nil
}

func validateObjects(ctx context.Context, client crclient.Client, before, after crclient.Object, operation string) error {
	if ctx == nil || nilInterface(client) || nilInterface(before) || nilInterface(after) {
		return invalid(operation)
	}
	if before.GetName() != after.GetName() || before.GetNamespace() != after.GetNamespace() {
		return faults.New(
			faults.CodeInvalidArgument,
			"Kubernetes patch snapshots identify different objects",
			faults.WithReason("patch_identity_mismatch"),
			faults.WithOperation(operation),
			faults.WithContextMetadata(ctx),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	if before.GetResourceVersion() == "" {
		return faults.New(
			faults.CodeFailedPrecondition,
			"Kubernetes optimistic patch requires a resource version",
			faults.WithReason("resource_version_missing"),
			faults.WithOperation(operation),
			faults.WithContextMetadata(ctx),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	return nil
}

func qualify(ctx context.Context, err error, operation string, object crclient.Object) error {
	reference := kubernetes.ReferenceFor(object, object.GetObjectKind().GroupVersionKind())
	return kubernetes.QualifyObject(ctx, err, operation, reference, nil)
}

func invalid(operation string) error {
	return faults.New(faults.CodeInvalidArgument, "invalid Kubernetes patch request", faults.WithReason("invalid_patch_request"), faults.WithOperation(operation), faults.WithRetryPolicy(faults.NoRetry()))
}

func nilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
