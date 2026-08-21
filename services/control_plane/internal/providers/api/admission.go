// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"go.mindclade.dev/control/admission"
	"go.mindclade.dev/libs/go/auth"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/httpx"
	"go.mindclade.dev/libs/go/httpx/middleware"
	"go.mindclade.dev/libs/go/idempotency"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/requestmeta"
	"go.mindclade.dev/libs/go/resourceversion"
	"go.mindclade.dev/services/control_plane/internal/transport"
)

const (
	reservationsPath     = "/v1/ai-gateway/reservations"
	reservationPrefix    = reservationsPath + "/"
	maximumAdmissionBody = 64 << 10
)

type admissionEngine interface {
	Admit(context.Context, admission.AdmitRequest) (admission.Decision, error)
	Commit(context.Context, identifiers.ID, resourceversion.Version, identifiers.Digest, string, admission.Quota) (admission.Decision, error)
	Release(context.Context, identifiers.ID, resourceversion.Version, identifiers.Digest, string) (admission.Decision, error)
}

type admitHTTPRequest struct {
	RequestDigest identifiers.Digest     `json:"request_digest"`
	Workspace     string                 `json:"workspace"`
	Route         admission.GatewayRoute `json:"route"`
	PolicyEpoch   uint64                 `json:"policy_epoch"`
	Requested     admission.Quota        `json:"requested"`
	TTLSeconds    uint32                 `json:"ttl_seconds"`
}

type finalizeHTTPRequest struct {
	RequestDigest identifiers.Digest `json:"request_digest"`
	Actual        admission.Quota    `json:"actual,omitempty"`
}

// reservationHTTPResponse intentionally omits idempotency keys and request
// digests. Those values prove mutation ownership inside the control plane and
// must not become response metadata copied into logs or traces.
type reservationHTTPResponse struct {
	ID                 identifiers.ID             `json:"id"`
	Workspace          string                     `json:"workspace"`
	Route              admission.GatewayRoute     `json:"route"`
	PolicyEpoch        uint64                     `json:"policy_epoch"`
	EntitlementID      identifiers.ID             `json:"entitlement_id"`
	EntitlementVersion resourceversion.Version    `json:"entitlement_version"`
	BudgetID           identifiers.ID             `json:"budget_id"`
	BudgetVersion      resourceversion.Version    `json:"budget_version"`
	Reserved           admission.Quota            `json:"reserved"`
	Actual             admission.Quota            `json:"actual,omitempty"`
	State              admission.ReservationState `json:"state"`
	CreatedAt          time.Time                  `json:"created_at"`
	ExpiresAt          time.Time                  `json:"expires_at"`
	FinalizedAt        *time.Time                 `json:"finalized_at,omitempty"`
	Version            resourceversion.Version    `json:"resource_version"`
}

type decisionHTTPResponse struct {
	Reservation reservationHTTPResponse `json:"reservation"`
	Replayed    bool                    `json:"replayed"`
}

func newAdmissionMux(engine admissionEngine) (http.Handler, error) {
	if engine == nil {
		return nil, admissionHTTPError(faults.CodeFailedPrecondition, "admission_engine_unconfigured", "admission engine is not configured")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+reservationsPath, func(writer http.ResponseWriter, request *http.Request) {
		request = withAdmissionOperation(request, "ai_gateway.reservations.create")
		principal, err := auth.RequirePrincipal(request.Context())
		if err != nil {
			httpx.WriteError(request.Context(), writer, err, request.URL.Path)
			return
		}
		key, err := parseIdempotencyHeader(request)
		if err != nil {
			httpx.WriteError(request.Context(), writer, err, request.URL.Path)
			return
		}
		var input admitHTTPRequest
		if err := httpx.DecodeJSON(request, &input, maximumAdmissionBody); err != nil {
			httpx.WriteError(request.Context(), writer, err, request.URL.Path)
			return
		}
		if input.TTLSeconds == 0 || input.TTLSeconds > uint32((24*time.Hour)/time.Second) {
			httpx.WriteError(request.Context(), writer,
				admissionHTTPError(faults.CodeInvalidArgument, "reservation_ttl_invalid", "reservation TTL is outside bounds"), request.URL.Path)
			return
		}
		scope, err := idempotency.ParseScope(input.Workspace + "/mlflow-gateway/" + principal.Subject())
		if err != nil {
			httpx.WriteError(request.Context(), writer, err, request.URL.Path)
			return
		}
		decision, err := engine.Admit(request.Context(), admission.AdmitRequest{
			Idempotency:   idempotency.Identity{Scope: scope, Key: key},
			RequestDigest: input.RequestDigest, Subject: principal.Subject(), Workspace: input.Workspace,
			Route: input.Route, PolicyEpoch: input.PolicyEpoch, Requested: input.Requested,
			TTL: time.Duration(input.TTLSeconds) * time.Second,
		})
		if err != nil {
			httpx.WriteError(request.Context(), writer, err, request.URL.Path)
			return
		}
		writer.Header().Set("Cache-Control", "no-store")
		writer.Header().Set("ETag", decision.Reservation.Version.ETag())
		writer.Header().Set("Location", reservationPrefix+decision.Reservation.ID.String())
		status := http.StatusCreated
		if decision.Replayed {
			status = http.StatusOK
		}
		if err := httpx.WriteJSON(writer, status, projectDecision(decision)); err != nil {
			httpx.WriteError(request.Context(), writer, err, request.URL.Path)
		}
	})
	mux.HandleFunc("POST "+reservationPrefix+"{reservationID}/commit", func(writer http.ResponseWriter, request *http.Request) {
		request = withAdmissionOperation(request, "ai_gateway.reservations.commit")
		principal, err := auth.RequirePrincipal(request.Context())
		if err != nil {
			httpx.WriteError(request.Context(), writer, err, request.URL.Path)
			return
		}
		reservationID, expected, input, err := decodeFinalizationRequest(request)
		if err != nil {
			httpx.WriteError(request.Context(), writer, err, request.URL.Path)
			return
		}
		decision, err := engine.Commit(request.Context(), reservationID, expected, input.RequestDigest, principal.Subject(), input.Actual)
		writeFinalization(writer, request, decision, err)
	})
	mux.HandleFunc("POST "+reservationPrefix+"{reservationID}/release", func(writer http.ResponseWriter, request *http.Request) {
		request = withAdmissionOperation(request, "ai_gateway.reservations.release")
		principal, err := auth.RequirePrincipal(request.Context())
		if err != nil {
			httpx.WriteError(request.Context(), writer, err, request.URL.Path)
			return
		}
		reservationID, expected, input, err := decodeFinalizationRequest(request)
		if err != nil {
			httpx.WriteError(request.Context(), writer, err, request.URL.Path)
			return
		}
		if len(input.Actual) != 0 {
			httpx.WriteError(request.Context(), writer,
				admissionHTTPError(faults.CodeInvalidArgument, "release_actual_unexpected", "release must not contain actual usage"), request.URL.Path)
			return
		}
		decision, err := engine.Release(request.Context(), reservationID, expected, input.RequestDigest, principal.Subject())
		writeFinalization(writer, request, decision, err)
	})
	return mux, nil
}

func decodeFinalizationRequest(request *http.Request) (identifiers.ID, resourceversion.Version, finalizeHTTPRequest, error) {
	var input finalizeHTTPRequest
	if len(request.Header.Values("If-Match")) > 1 || len(request.Header.Values("If-None-Match")) > 1 {
		return identifiers.ID{}, resourceversion.Version{}, input,
			admissionHTTPError(faults.CodeInvalidArgument, "conditional_header_ambiguous", "conditional request headers must not be repeated")
	}
	id, err := identifiers.ParseIDKind(request.PathValue("reservationID"), identifiers.MustParseKind("reservation"))
	if err != nil {
		return identifiers.ID{}, resourceversion.Version{}, input,
			admissionHTTPError(faults.CodeInvalidArgument, "reservation_id_invalid", "reservation ID is invalid")
	}
	precondition, present := transport.PreconditionFrom(request.Context())
	if !present || precondition.Match.IsZero() || precondition.MustExist || precondition.MustNotExist {
		return identifiers.ID{}, resourceversion.Version{}, input,
			admissionHTTPError(faults.CodeFailedPrecondition, "if_match_required", "an exact If-Match reservation version is required")
	}
	if err := httpx.DecodeJSON(request, &input, maximumAdmissionBody); err != nil {
		return identifiers.ID{}, resourceversion.Version{}, input, err
	}
	return id, precondition.Match, input, nil
}

func writeFinalization(writer http.ResponseWriter, request *http.Request, decision admission.Decision, err error) {
	if err != nil {
		httpx.WriteError(request.Context(), writer, err, request.URL.Path)
		return
	}
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("ETag", decision.Reservation.Version.ETag())
	if writeErr := httpx.WriteJSON(writer, http.StatusOK, projectDecision(decision)); writeErr != nil {
		httpx.WriteError(request.Context(), writer, writeErr, request.URL.Path)
	}
}

func projectDecision(decision admission.Decision) decisionHTTPResponse {
	reservation := decision.Reservation
	var finalizedAt *time.Time
	if !reservation.FinalizedAt.IsZero() {
		value := reservation.FinalizedAt
		finalizedAt = &value
	}
	return decisionHTTPResponse{
		Reservation: reservationHTTPResponse{
			ID: reservation.ID, Workspace: reservation.Workspace, Route: reservation.Route,
			PolicyEpoch: reservation.PolicyEpoch, EntitlementID: reservation.EntitlementID,
			EntitlementVersion: reservation.EntitlementVersion, BudgetID: reservation.BudgetID,
			BudgetVersion: reservation.BudgetVersion, Reserved: reservation.Reserved.Clone(),
			Actual: reservation.Actual.Clone(), State: reservation.State, CreatedAt: reservation.CreatedAt,
			ExpiresAt: reservation.ExpiresAt, FinalizedAt: finalizedAt, Version: reservation.Version,
		},
		Replayed: decision.Replayed,
	}
}

func parseIdempotencyHeader(request *http.Request) (idempotency.Key, error) {
	values := make([]string, 0, len(request.Header.Values(httpx.HeaderIdempotencyKey)))
	for _, value := range request.Header.Values(httpx.HeaderIdempotencyKey) {
		if normalized := strings.TrimSpace(value); normalized != "" {
			values = append(values, normalized)
		}
	}
	if len(values) != 1 {
		return idempotency.Key{}, admissionHTTPError(faults.CodeInvalidArgument, "idempotency_key_required", "exactly one idempotency key is required")
	}
	return idempotency.ParseKey(values[0])
}

func resolveAdmissionAuthorization(request *http.Request) (middleware.AuthorizationTarget, bool, error) {
	if request == nil {
		return middleware.AuthorizationTarget{}, false, admissionHTTPError(faults.CodeInvalidArgument, "authorization_request_nil", "authorization request is invalid")
	}
	permission := ""
	var resourceOptions []auth.ResourceOption
	switch {
	case request.Method == http.MethodPost && request.URL.Path == reservationsPath:
		permission = "ai_gateway.reservations.create"
	case request.Method == http.MethodPost && matchesAdmissionActionPath(request.URL.Path, "commit"):
		permission = "ai_gateway.reservations.commit"
		reservationID, err := admissionAuthorizationReservationID(request.URL.Path, "commit")
		if err != nil {
			return middleware.AuthorizationTarget{}, false, err
		}
		resourceOptions = append(resourceOptions, auth.WithResourceID(reservationID))
	case request.Method == http.MethodPost && matchesAdmissionActionPath(request.URL.Path, "release"):
		permission = "ai_gateway.reservations.release"
		reservationID, err := admissionAuthorizationReservationID(request.URL.Path, "release")
		if err != nil {
			return middleware.AuthorizationTarget{}, false, err
		}
		resourceOptions = append(resourceOptions, auth.WithResourceID(reservationID))
	default:
		return middleware.AuthorizationTarget{}, false, nil
	}
	parsedPermission, err := auth.ParsePermission(permission)
	if err != nil {
		return middleware.AuthorizationTarget{}, false, err
	}
	resourceType, err := auth.ParseResourceType("ai_gateway.reservation")
	if err != nil {
		return middleware.AuthorizationTarget{}, false, err
	}
	resource, err := auth.NewResource(resourceType, resourceOptions...)
	if err != nil {
		return middleware.AuthorizationTarget{}, false, err
	}
	return middleware.AuthorizationTarget{Permission: parsedPermission, Resource: resource}, true, nil
}

func matchesAdmissionActionPath(path, action string) bool {
	if !strings.HasPrefix(path, reservationPrefix) || !strings.HasSuffix(path, "/"+action) {
		return false
	}
	reservationID := strings.TrimSuffix(strings.TrimPrefix(path, reservationPrefix), "/"+action)
	return reservationID != "" && !strings.Contains(reservationID, "/")
}

func admissionAuthorizationReservationID(path, action string) (identifiers.ID, error) {
	value := strings.TrimSuffix(strings.TrimPrefix(path, reservationPrefix), "/"+action)
	reservationID, err := identifiers.ParseIDKind(value, identifiers.MustParseKind("reservation"))
	if err != nil {
		return identifiers.ID{}, admissionHTTPError(faults.CodeInvalidArgument, "reservation_id_invalid", "reservation ID is invalid")
	}
	return reservationID, nil
}

func withAdmissionOperation(request *http.Request, name string) *http.Request {
	operation, err := requestmeta.ParseOperation(name)
	if err != nil {
		return request
	}
	ctx, err := requestmeta.WithOperation(request.Context(), operation)
	if err != nil {
		return request
	}
	return request.WithContext(ctx)
}

func admissionHTTPError(code faults.Code, reason, message string) error {
	return faults.New(code, message, faults.WithReason(reason),
		faults.WithOperation("controlplane.api.admission"), faults.WithRetryPolicy(faults.NoRetry()))
}
