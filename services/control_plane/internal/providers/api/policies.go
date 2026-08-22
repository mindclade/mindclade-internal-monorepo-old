// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package api

import (
	"context"
	"net/http"
	"strings"

	"go.mindclade.dev/control/admission"
	"go.mindclade.dev/libs/go/auth"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/httpx"
	"go.mindclade.dev/libs/go/httpx/middleware"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/resourceversion"
	"go.mindclade.dev/services/control_plane/internal/transport"
)

const (
	policyWorkspacePrefix = "/v1/ai-gateway/workspaces/"
	maximumPolicyBody     = 1 << 20
)

type policyEngine interface {
	Propose(context.Context, auth.Principal, admission.WorkspacePolicyBundleSpec, resourceversion.Precondition) (admission.PolicyProposal, error)
	Proposal(context.Context, identifiers.ID) (admission.PolicyProposal, error)
	Bundle(context.Context, identifiers.ID) (admission.WorkspacePolicyBundle, error)
	Approve(context.Context, auth.Principal, identifiers.ID, resourceversion.Version) (admission.WorkspacePolicyBundle, admission.PolicyApprovalReceipt, error)
	Reject(context.Context, auth.Principal, identifiers.ID, resourceversion.Version) (admission.PolicyProposal, error)
	Cancel(context.Context, auth.Principal, identifiers.ID, resourceversion.Version) (admission.PolicyProposal, error)
}

type approvalHTTPResponse struct {
	Bundle  admission.WorkspacePolicyBundle `json:"bundle"`
	Receipt admission.PolicyApprovalReceipt `json:"receipt"`
}

func newPolicyMux(engine policyEngine) (http.Handler, error) {
	if engine == nil {
		return nil, policyHTTPError(faults.CodeFailedPrecondition, "policy_engine_unconfigured", "policy governance engine is not configured")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+policyWorkspacePrefix+"{workspaceID}/policy", func(writer http.ResponseWriter, request *http.Request) {
		request = withAdmissionOperation(request, "ai_gateway.policies.read")
		workspaceID, err := policyWorkspaceID(request)
		if err != nil {
			writePolicyError(writer, request, err)
			return
		}
		bundle, err := engine.Bundle(request.Context(), workspaceID)
		if err != nil {
			writePolicyError(writer, request, err)
			return
		}
		writer.Header().Set("Cache-Control", "no-store")
		writer.Header().Set("ETag", bundle.Version.ETag())
		writePolicyJSON(writer, request, http.StatusOK, bundle)
	})
	mux.HandleFunc("POST "+policyWorkspacePrefix+"{workspaceID}/policy-proposals", func(writer http.ResponseWriter, request *http.Request) {
		request = withAdmissionOperation(request, "ai_gateway.policy_proposals.create")
		principal, err := auth.RequirePrincipal(request.Context())
		if err != nil {
			writePolicyError(writer, request, err)
			return
		}
		workspaceID, err := policyWorkspaceID(request)
		if err != nil {
			writePolicyError(writer, request, err)
			return
		}
		precondition, err := exactPolicyRequestPrecondition(request, false)
		if err != nil {
			writePolicyError(writer, request, err)
			return
		}
		var spec admission.WorkspacePolicyBundleSpec
		if err := httpx.DecodeJSON(request, &spec, maximumPolicyBody); err != nil {
			writePolicyError(writer, request, err)
			return
		}
		if spec.WorkspaceID != workspaceID {
			writePolicyError(writer, request, policyHTTPError(faults.CodeInvalidArgument, "policy_workspace_mismatch", "policy workspace does not match the request path"))
			return
		}
		proposal, err := engine.Propose(request.Context(), principal, spec, precondition)
		if err != nil {
			writePolicyError(writer, request, err)
			return
		}
		writer.Header().Set("Cache-Control", "no-store")
		writer.Header().Set("ETag", proposal.Version.ETag())
		writer.Header().Set("Location", policyWorkspacePrefix+workspaceID.String()+"/policy-proposals/"+proposal.ID.String())
		writePolicyJSON(writer, request, http.StatusCreated, proposal)
	})
	mux.HandleFunc("GET "+policyWorkspacePrefix+"{workspaceID}/policy-proposals/{proposalID}", func(writer http.ResponseWriter, request *http.Request) {
		request = withAdmissionOperation(request, "ai_gateway.policy_proposals.read")
		workspaceID, proposalID, err := policyPathIDs(request)
		if err != nil {
			writePolicyError(writer, request, err)
			return
		}
		proposal, err := engine.Proposal(request.Context(), proposalID)
		if err != nil {
			writePolicyError(writer, request, err)
			return
		}
		if proposal.Spec.WorkspaceID != workspaceID {
			writePolicyError(writer, request, policyHTTPError(faults.CodeNotFound, "policy_proposal_not_found", "policy proposal was not found"))
			return
		}
		writer.Header().Set("Cache-Control", "no-store")
		writer.Header().Set("ETag", proposal.Version.ETag())
		writePolicyJSON(writer, request, http.StatusOK, proposal)
	})
	for _, action := range []string{"approval", "rejection", "cancellation"} {
		action := action
		mux.HandleFunc("POST "+policyWorkspacePrefix+"{workspaceID}/policy-proposals/{proposalID}/"+action, func(writer http.ResponseWriter, request *http.Request) {
			request = withAdmissionOperation(request, "ai_gateway.policy_proposals."+action)
			principal, err := auth.RequirePrincipal(request.Context())
			if err != nil {
				writePolicyError(writer, request, err)
				return
			}
			workspaceID, proposalID, err := policyPathIDs(request)
			if err != nil {
				writePolicyError(writer, request, err)
				return
			}
			expected, err := exactPolicyVersion(request)
			if err != nil {
				writePolicyError(writer, request, err)
				return
			}
			stored, err := engine.Proposal(request.Context(), proposalID)
			if err != nil {
				writePolicyError(writer, request, err)
				return
			}
			if stored.Spec.WorkspaceID != workspaceID {
				writePolicyError(writer, request, policyHTTPError(faults.CodeNotFound, "policy_proposal_not_found", "policy proposal was not found"))
				return
			}
			writer.Header().Set("Cache-Control", "no-store")
			switch action {
			case "approval":
				bundle, receipt, err := engine.Approve(request.Context(), principal, proposalID, expected)
				if err != nil {
					writePolicyError(writer, request, err)
					return
				}
				writer.Header().Set("ETag", bundle.Version.ETag())
				writePolicyJSON(writer, request, http.StatusOK, approvalHTTPResponse{Bundle: bundle, Receipt: receipt})
			case "rejection", "cancellation":
				var terminal admission.PolicyProposal
				if action == "rejection" {
					terminal, err = engine.Reject(request.Context(), principal, proposalID, expected)
				} else {
					terminal, err = engine.Cancel(request.Context(), principal, proposalID, expected)
				}
				if err != nil {
					writePolicyError(writer, request, err)
					return
				}
				writer.Header().Set("ETag", terminal.Version.ETag())
				writePolicyJSON(writer, request, http.StatusOK, terminal)
			}
		})
	}
	return mux, nil
}

func policyWorkspaceID(request *http.Request) (identifiers.ID, error) {
	id, err := identifiers.ParseIDKind(request.PathValue("workspaceID"), identifiers.MustParseKind("workspace"))
	if err != nil {
		return identifiers.ID{}, policyHTTPError(faults.CodeInvalidArgument, "policy_workspace_id_invalid", "workspace ID is invalid")
	}
	return id, nil
}

func policyPathIDs(request *http.Request) (identifiers.ID, identifiers.ID, error) {
	workspaceID, err := policyWorkspaceID(request)
	if err != nil {
		return identifiers.ID{}, identifiers.ID{}, err
	}
	proposalID, err := identifiers.ParseIDKind(request.PathValue("proposalID"), identifiers.MustParseKind("policyproposal"))
	if err != nil {
		return identifiers.ID{}, identifiers.ID{}, policyHTTPError(faults.CodeInvalidArgument, "policy_proposal_id_invalid", "policy proposal ID is invalid")
	}
	return workspaceID, proposalID, nil
}

func exactPolicyRequestPrecondition(request *http.Request, matchOnly bool) (resourceversion.Precondition, error) {
	if len(request.Header.Values("If-Match")) > 1 || len(request.Header.Values("If-None-Match")) > 1 {
		return resourceversion.Precondition{}, policyHTTPError(faults.CodeInvalidArgument, "conditional_header_ambiguous", "conditional request headers must not be repeated")
	}
	precondition, present := transport.PreconditionFrom(request.Context())
	if !present || precondition.MustExist || matchOnly && precondition.Match.IsZero() ||
		!matchOnly && !precondition.MustNotExist && precondition.Match.IsZero() {
		return resourceversion.Precondition{}, policyHTTPError(faults.CodeFailedPrecondition, "policy_precondition_required", "an exact policy precondition is required")
	}
	return precondition, nil
}

func exactPolicyVersion(request *http.Request) (resourceversion.Version, error) {
	precondition, err := exactPolicyRequestPrecondition(request, true)
	if err != nil {
		return resourceversion.Version{}, err
	}
	return precondition.Match, nil
}

func resolvePolicyAuthorization(request *http.Request) (middleware.AuthorizationTarget, bool, error) {
	if request == nil {
		return middleware.AuthorizationTarget{}, false, policyHTTPError(faults.CodeInvalidArgument, "authorization_request_nil", "authorization request is invalid")
	}
	workspaceID, proposalID, action, shape, err := parsePolicyAuthorizationPath(request.URL.Path)
	if err != nil {
		return middleware.AuthorizationTarget{}, false, err
	}
	permission := ""
	resourceType := "ai_gateway.workspace_policy"
	resourceID := workspaceID
	switch {
	case request.Method == http.MethodGet && shape == "policy":
		permission = "ai_gateway.policies.read"
	case request.Method == http.MethodPost && shape == "proposal_collection":
		permission = "ai_gateway.policy_proposals.create"
	case request.Method == http.MethodGet && shape == "proposal":
		permission = "ai_gateway.policy_proposals.read"
		resourceType, resourceID = "ai_gateway.policy_proposal", proposalID
	case request.Method == http.MethodPost && shape == "proposal_action":
		resourceType, resourceID = "ai_gateway.policy_proposal", proposalID
		switch action {
		case "approval":
			permission = "ai_gateway.policy_proposals.approve"
		case "rejection":
			permission = "ai_gateway.policy_proposals.reject"
		case "cancellation":
			permission = "ai_gateway.policy_proposals.cancel"
		}
	}
	if permission == "" {
		return middleware.AuthorizationTarget{}, false, nil
	}
	parsedPermission, err := auth.ParsePermission(permission)
	if err != nil {
		return middleware.AuthorizationTarget{}, false, err
	}
	parsedType, err := auth.ParseResourceType(resourceType)
	if err != nil {
		return middleware.AuthorizationTarget{}, false, err
	}
	resource, err := auth.NewResource(parsedType, auth.WithResourceID(resourceID),
		auth.WithResourceAttributes(map[string]string{"workspace_id": workspaceID.String()}))
	if err != nil {
		return middleware.AuthorizationTarget{}, false, err
	}
	return middleware.AuthorizationTarget{Permission: parsedPermission, Resource: resource}, true, nil
}

func parsePolicyAuthorizationPath(path string) (identifiers.ID, identifiers.ID, string, string, error) {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	if len(segments) < 5 || segments[0] != "v1" || segments[1] != "ai-gateway" || segments[2] != "workspaces" {
		return identifiers.ID{}, identifiers.ID{}, "", "", nil
	}
	workspaceID, err := identifiers.ParseIDKind(segments[3], identifiers.MustParseKind("workspace"))
	if err != nil {
		return identifiers.ID{}, identifiers.ID{}, "", "", policyHTTPError(faults.CodeInvalidArgument, "policy_workspace_id_invalid", "workspace ID is invalid")
	}
	if len(segments) == 5 && segments[4] == "policy" {
		return workspaceID, identifiers.ID{}, "", "policy", nil
	}
	if segments[4] != "policy-proposals" {
		return identifiers.ID{}, identifiers.ID{}, "", "", nil
	}
	if len(segments) == 5 {
		return workspaceID, identifiers.ID{}, "", "proposal_collection", nil
	}
	if len(segments) != 6 && len(segments) != 7 {
		return identifiers.ID{}, identifiers.ID{}, "", "", nil
	}
	proposalID, err := identifiers.ParseIDKind(segments[5], identifiers.MustParseKind("policyproposal"))
	if err != nil {
		return identifiers.ID{}, identifiers.ID{}, "", "", policyHTTPError(faults.CodeInvalidArgument, "policy_proposal_id_invalid", "policy proposal ID is invalid")
	}
	if len(segments) == 6 {
		return workspaceID, proposalID, "", "proposal", nil
	}
	return workspaceID, proposalID, segments[6], "proposal_action", nil
}

func writePolicyJSON(writer http.ResponseWriter, request *http.Request, status int, value any) {
	if err := httpx.WriteJSON(writer, status, value); err != nil {
		httpx.WriteError(request.Context(), writer, err, request.URL.Path)
	}
}

func writePolicyError(writer http.ResponseWriter, request *http.Request, err error) {
	httpx.WriteError(request.Context(), writer, err, request.URL.Path)
}

func policyHTTPError(code faults.Code, reason, message string) error {
	return faults.New(code, message, faults.WithReason(reason), faults.WithOperation("controlplane.api.policies"), faults.WithRetryPolicy(faults.NoRetry()))
}
