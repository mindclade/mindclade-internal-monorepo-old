// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

package api

import (
	"context"
	"net/http"
	"strings"

	"go.mindclade.dev/control/evidence"
	"go.mindclade.dev/libs/go/auth"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/httpx"
	"go.mindclade.dev/libs/go/httpx/middleware"
	"go.mindclade.dev/libs/go/identifiers"
)

const maximumEvidenceBody = 1 << 20

type eligibilityEngine interface {
	Record(context.Context, evidence.Claim, evidence.Verification) error
	Evidence(context.Context, identifiers.Digest) ([]evidence.Record, error)
	Evaluate(context.Context, evidence.DeploymentBundle) (evidence.SignedDecision, error)
	GetDecision(context.Context, identifiers.Digest) (evidence.SignedDecision, bool, error)
	Revoke(context.Context, identifiers.Digest, string) error
}

type eligibilityDecisionHTTPResponse struct {
	SignedDecision evidence.SignedDecision `json:"signed_decision"`
	Revoked        bool                    `json:"revoked"`
}

type evidenceHTTPResponse struct {
	Records []evidence.Record `json:"records"`
}

type revocationHTTPRequest struct {
	Reason string `json:"reason"`
}

func newEligibilityMux(engine eligibilityEngine) (http.Handler, error) {
	if engine == nil {
		return nil, eligibilityHTTPError(faults.CodeFailedPrecondition, "eligibility_engine_unconfigured", "production eligibility engine is not configured")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/evidence/claims", func(writer http.ResponseWriter, request *http.Request) {
		var record evidence.Record
		if err := httpx.DecodeJSON(request, &record, maximumEvidenceBody); err != nil {
			writeEligibilityError(writer, request, err)
			return
		}
		if err := engine.Record(request.Context(), record.Claim, record.Verification); err != nil {
			writeEligibilityError(writer, request, err)
			return
		}
		writer.Header().Set("Cache-Control", "no-store")
		writer.Header().Set("Location", "/v1/evidence/subjects/"+record.Claim.SubjectDigest.String()+"/claims")
		writeEligibilityJSON(writer, request, http.StatusCreated, record)
	})
	mux.HandleFunc("GET /v1/evidence/subjects/{subjectDigest}/claims", func(writer http.ResponseWriter, request *http.Request) {
		digest, err := eligibilityPathDigest(request, "subjectDigest")
		if err != nil {
			writeEligibilityError(writer, request, err)
			return
		}
		records, err := engine.Evidence(request.Context(), digest)
		if err != nil {
			writeEligibilityError(writer, request, err)
			return
		}
		writer.Header().Set("Cache-Control", "no-store")
		writeEligibilityJSON(writer, request, http.StatusOK, evidenceHTTPResponse{Records: records})
	})
	mux.HandleFunc("POST /v1/production-eligibility/decisions", func(writer http.ResponseWriter, request *http.Request) {
		var bundle evidence.DeploymentBundle
		if err := httpx.DecodeJSON(request, &bundle, maximumEvidenceBody); err != nil {
			writeEligibilityError(writer, request, err)
			return
		}
		decision, err := engine.Evaluate(request.Context(), bundle)
		if err != nil {
			writeEligibilityError(writer, request, err)
			return
		}
		writer.Header().Set("Cache-Control", "no-store")
		writer.Header().Set("Location", "/v1/production-eligibility/decisions/"+decision.Decision.DecisionDigest.String())
		writeEligibilityJSON(writer, request, http.StatusCreated, eligibilityDecisionHTTPResponse{SignedDecision: decision})
	})
	mux.HandleFunc("GET /v1/production-eligibility/decisions/{decisionDigest}", func(writer http.ResponseWriter, request *http.Request) {
		digest, err := eligibilityPathDigest(request, "decisionDigest")
		if err != nil {
			writeEligibilityError(writer, request, err)
			return
		}
		decision, revoked, err := engine.GetDecision(request.Context(), digest)
		if err != nil {
			writeEligibilityError(writer, request, err)
			return
		}
		writer.Header().Set("Cache-Control", "no-store")
		writeEligibilityJSON(writer, request, http.StatusOK, eligibilityDecisionHTTPResponse{SignedDecision: decision, Revoked: revoked})
	})
	mux.HandleFunc("POST /v1/production-eligibility/decisions/{decisionDigest}/revocations", func(writer http.ResponseWriter, request *http.Request) {
		digest, err := eligibilityPathDigest(request, "decisionDigest")
		if err != nil {
			writeEligibilityError(writer, request, err)
			return
		}
		var input revocationHTTPRequest
		if err := httpx.DecodeJSON(request, &input, maximumEvidenceBody); err != nil {
			writeEligibilityError(writer, request, err)
			return
		}
		if err := engine.Revoke(request.Context(), digest, input.Reason); err != nil {
			writeEligibilityError(writer, request, err)
			return
		}
		writer.Header().Set("Cache-Control", "no-store")
		writer.WriteHeader(http.StatusNoContent)
	})
	return mux, nil
}

func newAdminMux(policies policyEngine, eligibility eligibilityEngine) (http.Handler, error) {
	policyMux, err := newPolicyMux(policies)
	if err != nil {
		return nil, err
	}
	if eligibility == nil {
		return policyMux, nil
	}
	eligibilityMux, err := newEligibilityMux(eligibility)
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.Handle("/v1/evidence/", eligibilityMux)
	mux.Handle("/v1/production-eligibility/", eligibilityMux)
	mux.Handle("/", policyMux)
	return mux, nil
}

func resolveAdminAuthorization(request *http.Request) (middleware.AuthorizationTarget, bool, error) {
	if request != nil && (strings.HasPrefix(request.URL.Path, "/v1/evidence/") || strings.HasPrefix(request.URL.Path, "/v1/production-eligibility/")) {
		return resolveEligibilityAuthorization(request)
	}
	return resolvePolicyAuthorization(request)
}

func resolveEligibilityAuthorization(request *http.Request) (middleware.AuthorizationTarget, bool, error) {
	if request == nil {
		return middleware.AuthorizationTarget{}, false, eligibilityHTTPError(faults.CodeInvalidArgument, "authorization_request_nil", "authorization request is invalid")
	}
	permission := ""
	resourceType := ""
	attributes := map[string]string{}
	segments := strings.Split(strings.Trim(request.URL.Path, "/"), "/")
	switch {
	case request.Method == http.MethodPost && request.URL.Path == "/v1/evidence/claims":
		permission, resourceType = "evidence.claims.submit", "evidence.claim"
	case request.Method == http.MethodGet && len(segments) == 5 && segments[0] == "v1" && segments[1] == "evidence" && segments[2] == "subjects" && segments[4] == "claims":
		digest, err := identifiers.ParseDigest(segments[3])
		if err != nil {
			return middleware.AuthorizationTarget{}, false, eligibilityHTTPError(faults.CodeInvalidArgument, "evidence_subject_digest_invalid", "evidence subject digest is invalid")
		}
		permission, resourceType = "evidence.claims.read", "evidence.subject"
		attributes["subject_digest"] = digest.String()
	case request.Method == http.MethodPost && request.URL.Path == "/v1/production-eligibility/decisions":
		permission, resourceType = "production_eligibility.decisions.evaluate", "production_eligibility.decision"
	case len(segments) >= 4 && segments[0] == "v1" && segments[1] == "production-eligibility" && segments[2] == "decisions":
		digest, err := identifiers.ParseDigest(segments[3])
		if err != nil {
			return middleware.AuthorizationTarget{}, false, eligibilityHTTPError(faults.CodeInvalidArgument, "eligibility_decision_digest_invalid", "eligibility decision digest is invalid")
		}
		attributes["decision_digest"] = digest.String()
		resourceType = "production_eligibility.decision"
		switch {
		case request.Method == http.MethodGet && len(segments) == 4:
			permission = "production_eligibility.decisions.read"
		case request.Method == http.MethodPost && len(segments) == 5 && segments[4] == "revocations":
			permission = "production_eligibility.decisions.revoke"
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
	resource, err := auth.NewResource(parsedType, auth.WithResourceAttributes(attributes))
	if err != nil {
		return middleware.AuthorizationTarget{}, false, err
	}
	return middleware.AuthorizationTarget{Permission: parsedPermission, Resource: resource}, true, nil
}

func eligibilityPathDigest(request *http.Request, name string) (identifiers.Digest, error) {
	digest, err := identifiers.ParseDigest(request.PathValue(name))
	if err != nil {
		return identifiers.Digest{}, eligibilityHTTPError(faults.CodeInvalidArgument, "eligibility_digest_invalid", "evidence digest is invalid")
	}
	return digest, nil
}

func writeEligibilityJSON(writer http.ResponseWriter, request *http.Request, status int, value any) {
	if err := httpx.WriteJSON(writer, status, value); err != nil {
		httpx.WriteError(request.Context(), writer, err, request.URL.Path)
	}
}

func writeEligibilityError(writer http.ResponseWriter, request *http.Request, err error) {
	httpx.WriteError(request.Context(), writer, err, request.URL.Path)
}

func eligibilityHTTPError(code faults.Code, reason, message string) error {
	return faults.New(code, message, faults.WithReason(reason), faults.WithOperation("controlplane.api.eligibility"), faults.WithRetryPolicy(faults.NoRetry()))
}
