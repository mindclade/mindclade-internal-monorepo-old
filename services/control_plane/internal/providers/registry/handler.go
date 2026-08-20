// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

package registry

import (
	"net/http"
	"strings"

	"go.mindclade.dev/control/registry/models"
	"go.mindclade.dev/control/registry/releases"
	"go.mindclade.dev/libs/go/auth"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/httpx"
	"go.mindclade.dev/libs/go/httpx/middleware"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/requestmeta"
)

const (
	modelsPath       = "/v1/registry/models"
	modelPathPrefix  = modelsPath + "/"
	releasePathStart = "/v1/registry/releases/"
	releasePathEnd   = "/promotion"
)

type promoteReleaseRequest struct {
	Release       releases.Release       `json:"release"`
	EvidenceGraph releases.EvidenceGraph `json:"evidence_graph"`
}

func newRegistryMux(engine domains) (http.Handler, error) {
	if engine.models == nil || engine.releases == nil {
		return nil, faults.New(faults.CodeFailedPrecondition, "registry domain engines are not configured",
			faults.WithReason("registry_domain_unconfigured"),
			faults.WithOperation("controlplane.registry.newRegistryMux"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+modelsPath, func(writer http.ResponseWriter, request *http.Request) {
		request = withOperation(request, "registry.models.publish")
		var descriptor models.Descriptor
		if err := httpx.DecodeJSON(request, &descriptor, maximumRequestBody); err != nil {
			httpx.WriteError(request.Context(), writer, err, request.URL.Path)
			return
		}
		// The server is the sealing authority. Accepting a caller-supplied seal
		// makes it ambiguous whether the request is publish or import.
		if descriptor.DescriptorDigest.Valid() {
			httpx.WriteError(request.Context(), writer, invalidRequest("model_descriptor_digest_must_be_empty"), request.URL.Path)
			return
		}
		published, err := engine.models.Publish(request.Context(), descriptor)
		if err != nil {
			httpx.WriteError(request.Context(), writer, err, request.URL.Path)
			return
		}
		writer.Header().Set("Location", modelPathPrefix+published.DescriptorDigest.String())
		if err := httpx.WriteJSON(writer, http.StatusCreated, published); err != nil {
			httpx.WriteError(request.Context(), writer, err, request.URL.Path)
		}
	})
	mux.HandleFunc("GET "+modelPathPrefix+"{digest}", func(writer http.ResponseWriter, request *http.Request) {
		request = withOperation(request, "registry.models.resolve")
		digest, err := identifiers.ParseDigest(request.PathValue("digest"))
		if err != nil {
			httpx.WriteError(request.Context(), writer, faults.Wrap(err, faults.CodeInvalidArgument, "invalid model descriptor digest",
				faults.WithReason("model_descriptor_digest_invalid"), faults.WithRetryPolicy(faults.NoRetry())), request.URL.Path)
			return
		}
		descriptor, err := engine.models.Resolve(request.Context(), digest)
		if err != nil {
			httpx.WriteError(request.Context(), writer, err, request.URL.Path)
			return
		}
		writer.Header().Set("Cache-Control", "private, max-age=60")
		writer.Header().Set("ETag", `"`+descriptor.DescriptorDigest.String()+`"`)
		if err := httpx.WriteJSON(writer, http.StatusOK, descriptor); err != nil {
			httpx.WriteError(request.Context(), writer, err, request.URL.Path)
		}
	})
	mux.HandleFunc("POST "+releasePathStart+"{releaseID}"+releasePathEnd, func(writer http.ResponseWriter, request *http.Request) {
		request = withOperation(request, "registry.releases.promote")
		var input promoteReleaseRequest
		if err := httpx.DecodeJSON(request, &input, maximumRequestBody); err != nil {
			httpx.WriteError(request.Context(), writer, err, request.URL.Path)
			return
		}
		pathID := request.PathValue("releaseID")
		if input.Release.ReleaseID != pathID || input.EvidenceGraph.ReleaseID != pathID {
			httpx.WriteError(request.Context(), writer, invalidRequest("release_identity_mismatch"), request.URL.Path)
			return
		}
		if err := engine.releases.Promote(request.Context(), input.Release, input.EvidenceGraph); err != nil {
			httpx.WriteError(request.Context(), writer, err, request.URL.Path)
			return
		}
		if err := httpx.WriteJSON(writer, http.StatusNoContent, nil); err != nil {
			httpx.WriteError(request.Context(), writer, err, request.URL.Path)
		}
	})
	return mux, nil
}

// resolveAuthorization maps every exposed route to an explicit permission.
// RequireMapping is enabled by the caller, so a newly mounted route is denied
// until this switch is extended deliberately.
func resolveAuthorization(request *http.Request) (middleware.AuthorizationTarget, bool, error) {
	if request == nil {
		return middleware.AuthorizationTarget{}, false, invalidRequest("nil_authorization_request")
	}
	var permission, resourceType string
	attributes := map[string]string{}
	switch {
	case request.Method == http.MethodPost && request.URL.Path == modelsPath:
		permission, resourceType = "registry.models.publish", "registry.model"
	case request.Method == http.MethodGet && strings.HasPrefix(request.URL.Path, modelPathPrefix):
		permission, resourceType = "registry.models.read", "registry.model"
		attributes["digest"] = strings.TrimPrefix(request.URL.Path, modelPathPrefix)
	case request.Method == http.MethodPost && strings.HasPrefix(request.URL.Path, releasePathStart) && strings.HasSuffix(request.URL.Path, releasePathEnd):
		permission, resourceType = "registry.releases.promote", "registry.release"
		attributes["release_id"] = strings.TrimSuffix(strings.TrimPrefix(request.URL.Path, releasePathStart), releasePathEnd)
	default:
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

func withOperation(request *http.Request, name string) *http.Request {
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

func invalidRequest(reason string) error {
	return faults.New(faults.CodeInvalidArgument, "invalid registry request",
		faults.WithReason(reason),
		faults.WithOperation("controlplane.registry.http"),
		faults.WithRetryPolicy(faults.NoRetry()),
	)
}
