// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mindclade.internal/libs/go/auth"
	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/httpx"
	"mindclade.internal/libs/go/requestmeta"
)

func TestRequestMetadataGeneratesAndReturnsID(t *testing.T) {
	handler := RequestMetadata(nil)(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if _, ok := requestmeta.RequestIDFromContext(request.Context()); !ok {
			t.Error("request ID missing")
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/runs", nil))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d", recorder.Code)
	}
	if recorder.Header().Get(httpx.HeaderRequestID) == "" {
		t.Fatal("response request ID missing")
	}
}

func TestAuthenticationBearer(t *testing.T) {
	principal, err := auth.NewPrincipal(auth.PrincipalKindService, "gateway", auth.WithIssuer("mindclade.test"))
	if err != nil {
		t.Fatal(err)
	}
	authenticator := auth.AuthenticatorFunc(func(_ context.Context, credential auth.Credential) (auth.Principal, error) {
		if credential.Scheme() != auth.CredentialSchemeBearer || string(credential.Value()) != "token" {
			return auth.Principal{}, faults.New(faults.CodeUnauthenticated, "invalid credentials")
		}
		return principal, nil
	})
	handler := Authentication(AuthenticationConfig{Authenticator: authenticator})(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if _, ok := auth.PrincipalFromContext(request.Context()); !ok {
			t.Error("principal missing")
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Authorization", "Bearer token")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestAuthenticationRejectsMultipleCredentials(t *testing.T) {
	handler := Authentication(AuthenticationConfig{})(http.NotFoundHandler())
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Authorization", "Bearer token")
	request.Header.Set(httpx.HeaderAPIKey, "key")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", recorder.Code)
	}
}

func TestRecoverySanitizesPanic(t *testing.T) {
	handler := Recovery(nil)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("secret") }))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", recorder.Code)
	}
	if strings.Contains(recorder.Body.String(), "secret") {
		t.Fatal("panic leaked")
	}
}

func TestMaximumBodyRejectsDeclaredSize(t *testing.T) {
	handler := MaximumBody(2)(http.NotFoundHandler())
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("abc"))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d", recorder.Code)
	}
}

func TestServerStackOrdersLineageAccessSecurityAndRecovery(t *testing.T) {
	var observed AccessEvent
	handler := Server(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("private panic") }),
		StackConfig{
			AccessObserver: AccessObserverFunc(func(event AccessEvent) { observed = event }),
		},
	)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/runs", nil))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", recorder.Code)
	}
	if observed.Status != http.StatusInternalServerError {
		t.Fatalf("observed status = %d", observed.Status)
	}
	if _, ok := requestmeta.RequestIDFromContext(observed.Context); !ok {
		t.Fatal("access observer did not receive request lineage")
	}
	if recorder.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("security headers missing from recovered response")
	}
	if strings.Contains(recorder.Body.String(), "private panic") {
		t.Fatal("panic leaked")
	}
}

func TestAuthenticationRejectsDuplicateAuthorizationValues(t *testing.T) {
	handler := Authentication(AuthenticationConfig{})(http.NotFoundHandler())
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Add("Authorization", "Bearer one")
	request.Header.Add("Authorization", "Bearer two")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
}

type typedNilHTTPAuthorizationResolver struct{}

func (*typedNilHTTPAuthorizationResolver) ResolveAuthorization(*http.Request) (AuthorizationTarget, bool, error) {
	panic("typed nil resolver called")
}

func TestAuthorizationTreatsTypedNilResolverAsMissing(t *testing.T) {
	var resolver *typedNilHTTPAuthorizationResolver
	handler := Authorization(AuthorizationConfig{Resolver: resolver, RequireMapping: true})(http.NotFoundHandler())
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
}
