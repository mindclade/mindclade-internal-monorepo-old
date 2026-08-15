// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package httpx

import (
	"context"
	"log/slog"
	"net/http"

	"go.mindclade.dev/services/studio/internal/iap"
	"go.mindclade.dev/services/studio/internal/session"
)

type ctxKey int

const (
	ctxPrincipal ctxKey = iota
	ctxNonce
)

// WithNonce attaches the per-response CSP nonce so the shell renderer can put
// it on its script tags.
func WithNonce(r *http.Request, nonce string) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), ctxNonce, nonce))
}

// Nonce returns the CSP nonce for this response.
func Nonce(r *http.Request) string {
	n, _ := r.Context().Value(ctxNonce).(string)
	return n
}

// Principal returns the authenticated subject, if the request passed Authenticate.
func Principal(r *http.Request) (string, bool) {
	p, ok := r.Context().Value(ctxPrincipal).(string)
	return p, ok && p != ""
}

// Resolver resolves a verified IAP identity into an authorization decision.
//
// Called only when there is no valid session — its result is what the session
// cookie caches. It must be computable quickly enough to run on every cache
// miss, which is what bounds how much can live behind it.
type Resolver func(ctx context.Context, assertion iap.Assertion) error

// Authenticate is the two-layer gate, and the ordering matters.
//
// IAP is the NETWORK gate: it proves some org member is behind the request and
// injects a signed assertion. The BFF owns APPLICATION authorization and
// verifies that assertion cryptographically. Neither substitutes for the other.
//
//  1. Verify the IAP assertion — signature, issuer, audience. Never trust the
//     header on sight; anything reaching the pod directly can forge it.
//  2. Open the session cookie, BOUND TO THE SAME SUBJECT. A cookie for another
//     principal fails here, which is what makes a client-held session safe.
//  3. No valid session: resolve authorization and issue a fresh cookie.
//
// A failure at step 1 is a 401 and never a redirect. The stream client reads
// status codes, and an HTML sign-in page returned with 200 is indistinguishable
// from a stream to a client that cannot.
func Authenticate(v *iap.Verifier, codec *session.Codec, resolve Resolver, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assertion, err := v.FromRequest(r)
			if err != nil {
				// Deliberately terse. Which of signature, issuer, or audience
				// failed is a detail for our logs, not for the caller.
				logger.Warn("iap assertion rejected", "error", err, "path", r.URL.Path)
				http.Error(w, "unauthenticated", http.StatusUnauthorized)
				return
			}

			if cookie, cerr := r.Cookie(session.CookieName); cerr == nil {
				if claims, oerr := codec.Open(cookie.Value, assertion.Subject); oerr == nil {
					// Refresh silently. The five-minute TTL is the revocation
					// bound; sliding it on each request keeps an active user
					// signed in without widening that bound, because every
					// refresh re-checks the live assertion.
					issue(w, codec, claims.Subject, claims.SessionID, logger)
					next.ServeHTTP(w, withPrincipal(r, claims.Subject))
					return
				} else if oerr != nil {
					// Expected on expiry and after key rotation. Counted rather
					// than logged per-request: a non-zero rate right after a
					// rotation means the key overlap is too short, and a
					// non-zero rate otherwise means a bug or someone probing.
					sessionDecryptFailures.Add(1)
				}
			}

			if err := resolve(r.Context(), assertion); err != nil {
				logger.Info("authorization denied", "subject", assertion.Subject, "error", err)
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}

			sessionID, err := newSessionID()
			if err != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			issue(w, codec, assertion.Subject, sessionID, logger)
			next.ServeHTTP(w, withPrincipal(r, assertion.Subject))
		})
	}
}

func withPrincipal(r *http.Request, subject string) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), ctxPrincipal, subject))
}

// issue sets the session cookie.
//
// Every attribute is load-bearing:
//
//	__Host- prefix   The browser REFUSES the cookie unless it is Secure, has
//	                 Path=/, and has NO Domain. The last is the useful one — it
//	                 makes widening the scope to a parent domain impossible
//	                 later, even by accident.
//	SameSite=Lax     Not Strict. The handoff link is a cross-site top-level GET
//	                 from docs; Strict would drop the cookie on that first hop
//	                 and force a redundant round trip. Lax still withholds it
//	                 from every cross-site subresource and every cross-site POST,
//	                 which is the protection that matters.
//	HttpOnly         Script cannot read it. With no bearer token in the browser
//	                 either, XSS has nothing to steal.
func issue(w http.ResponseWriter, codec *session.Codec, subject, sessionID string, logger *slog.Logger) {
	value, err := codec.Seal(subject, sessionID)
	if err != nil {
		logger.Error("seal session", "error", err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     session.CookieName,
		Value:    value,
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(session.TTL.Seconds()),
		// No Domain, and the __Host- prefix means the browser enforces that
		// rather than trusting us to remember.
	})
}
