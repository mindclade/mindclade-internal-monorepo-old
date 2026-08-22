// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

package admission

import (
	"context"
	"slices"

	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/resourceversion"
)

// ResolvedEndpoint is the complete non-secret policy projection a gateway
// transport must match before it may reserve or contact a provider.
type ResolvedEndpoint struct {
	PolicyEpoch           uint64                  `json:"policy_epoch"`
	BundleVersion         resourceversion.Version `json:"bundle_version"`
	Endpoint              EndpointPolicy          `json:"endpoint"`
	SubjectMaximumRequest Quota                   `json:"subject_maximum_request"`
}

type bundleReader interface {
	CurrentBundle(context.Context, identifiers.ID) (WorkspacePolicyBundle, error)
}

// ResolveEndpoint authorizes a subject against the current atomic workspace
// bundle and projects only the endpoint data needed by the transport. It
// deliberately returns no provider credential material.
func (service Service) ResolveEndpoint(ctx context.Context, subject, workspace, endpoint string, operation GatewayOperation) (ResolvedEndpoint, error) {
	if err := service.validateLifecycle(ctx, subject); err != nil {
		return ResolvedEndpoint{}, err
	}
	if err := validateName(workspace, "workspace"); err != nil {
		return ResolvedEndpoint{}, err
	}
	if err := validateName(endpoint, "endpoint"); err != nil {
		return ResolvedEndpoint{}, err
	}
	if !operation.Valid() {
		return ResolvedEndpoint{}, invalid("endpoint_operation_invalid", "endpoint operation is invalid", nil)
	}
	reader, ok := service.Repository.(bundleReader)
	if !ok || nilInterface(reader) {
		return ResolvedEndpoint{}, unavailable("policy_bundle_repository_unavailable", "endpoint policy resolution is unavailable", nil)
	}
	workspaceID, err := identifiers.ParseIDKind(workspace, identifiers.MustParseKind("workspace"))
	if err != nil {
		return ResolvedEndpoint{}, invalid("workspace_id_invalid", "workspace ID is invalid", err)
	}
	now := service.now()
	snapshot, err := service.Repository.Snapshot(ctx, subject, workspace)
	if err != nil {
		return ResolvedEndpoint{}, err
	}
	if err := snapshot.Validate(); err != nil {
		return ResolvedEndpoint{}, err
	}
	if !snapshot.Entitlement.ActiveAt(now) {
		return ResolvedEndpoint{}, denied("entitlement_inactive", "entitlement is not active")
	}
	bundle, err := reader.CurrentBundle(ctx, workspaceID)
	if err != nil {
		return ResolvedEndpoint{}, err
	}
	if err := bundle.Validate(); err != nil {
		return ResolvedEndpoint{}, err
	}
	if now.Before(bundle.Spec.EffectiveAt) || !now.Before(bundle.Spec.ExpiresAt) {
		return ResolvedEndpoint{}, denied("policy_bundle_inactive", "workspace policy bundle is not active")
	}
	if snapshot.Entitlement.PolicyEpoch != bundle.Spec.PolicyEpoch {
		return ResolvedEndpoint{}, denied("policy_epoch_inconsistent", "entitlement and workspace policy epochs do not match")
	}
	policy, exists := bundle.Endpoint(endpoint, operation)
	if !exists || !snapshot.Entitlement.Allows(policy.Route) {
		return ResolvedEndpoint{}, denied("endpoint_not_entitled", "endpoint is not entitled")
	}
	matchedSubject := false
	for _, configured := range bundle.Spec.Subjects {
		if configured.Subject != subject {
			continue
		}
		matchedSubject = slices.Contains(configured.Endpoints, endpoint) && configured.MaximumRequest.Equal(snapshot.Entitlement.MaximumRequest)
		break
	}
	if !matchedSubject {
		return ResolvedEndpoint{}, denied("subject_policy_inconsistent", "subject entitlement does not match the workspace policy bundle")
	}
	return ResolvedEndpoint{
		PolicyEpoch:           bundle.Spec.PolicyEpoch,
		BundleVersion:         bundle.Version,
		Endpoint:              policy,
		SubjectMaximumRequest: snapshot.Entitlement.MaximumRequest.Clone(),
	}, nil
}
