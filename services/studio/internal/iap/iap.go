// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package iap verifies the assertion IAP injects on every request.
//
// # Never trust the header on sight
//
// x-goog-iap-jwt-assertion arrives as an ordinary request header. Anything that
// reaches the pod directly — another workload in the cluster, a compromised
// sidecar, a debug port-forward — can set it to whatever it likes. Reading the
// subject out of it without checking the signature is not authentication; it is
// accepting a claim from the caller.
//
// A NetworkPolicy restricts pod ingress to the load balancer's proxy subnet,
// and that is the other half of this control. Neither substitutes for the
// other: a policy is one kubectl apply from being edited, and a legitimate path
// into the namespace should still not confer the ability to forge identity.
//
// The acceptance check is explicit and worth keeping runnable: a request
// carrying `x-goog-iap-jwt-assertion: not.a.jwt`, sent from inside the cluster
// straight at the BFF, must return 401 and never 200.
//
// # Three things are checked, and all three matter
//
//	signature   ES256 over Google's published IAP keys. Without it, anyone can
//	            mint an assertion.
//	issuer      https://cloud.google.com/iap. Without it, a token from another
//	            Google service is accepted.
//	audience    The exact backend service this request reached. Without it, an
//	            assertion minted for a DIFFERENT IAP-protected application in
//	            the same organization is accepted here — which is the subtle one,
//	            because such a token is genuinely signed by Google and genuinely
//	            issued by IAP.
package iap

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

// HeaderName is where IAP places the assertion.
const HeaderName = "x-goog-iap-jwt-assertion"

// Issuer is the only issuer accepted.
const Issuer = "https://cloud.google.com/iap"

// KeysURL serves IAP's public keys. These are ES256 keys in a JWK-like map
// keyed by kid, NOT the OAuth2 certificate endpoint used for other Google
// tokens — pointing at the wrong one yields a key set in which no kid ever
// matches, and the failure reads as "unknown key" rather than "wrong endpoint".
const KeysURL = "https://www.gstatic.com/iap/verify/public_key-jwk"

var (
	ErrMissing    = errors.New("iap: no assertion header")
	ErrMalformed  = errors.New("iap: malformed assertion")
	ErrSignature  = errors.New("iap: signature verification failed")
	ErrIssuer     = errors.New("iap: unexpected issuer")
	ErrAudience   = errors.New("iap: unexpected audience")
	ErrExpired    = errors.New("iap: assertion expired")
	ErrUnknownKey = errors.New("iap: assertion names an unknown key")
)

// Assertion is the verified content of the IAP JWT.
type Assertion struct {
	// Subject is a STABLE, OPAQUE identifier for the principal, of the form
	// accounts.google.com:<numeric>. It is what the session cookie binds to.
	//
	// Bind to this and never to Email: an address can be reassigned to a
	// different person, and binding to it would silently transfer a session.
	Subject string

	// Email is for display and audit only. Never an authorization input.
	Email string

	IssuedAt  time.Time
	ExpiresAt time.Time
}

// Verifier checks assertions against IAP's published keys.
type Verifier struct {
	// audience is the exact expected value. For a Gateway backend it is
	//   /projects/<project-number>/global/backendServices/<backend-service-id>
	// Constructed from values Terraform knows and the workload does not, which
	// is why it is configuration rather than something derived at runtime.
	audience string

	keysURL string
	client  *http.Client

	mu        sync.RWMutex
	keys      map[string]*ecdsa.PublicKey
	fetchedAt time.Time
	ttl       time.Duration

	now func() time.Time
}

// NewVerifier returns a Verifier for one audience.
func NewVerifier(audience string, client *http.Client) (*Verifier, error) {
	if audience == "" {
		// An empty audience would make every check below pass for any
		// IAP-issued token in the organization. Refuse to start rather than
		// run with the weakest of the three checks silently disabled.
		return nil, errors.New("iap: audience is required; without it an assertion for any IAP application in the organization would be accepted")
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &Verifier{
		audience: audience,
		keysURL:  KeysURL,
		client:   client,
		keys:     map[string]*ecdsa.PublicKey{},
		ttl:      time.Hour,
		now:      time.Now,
	}, nil
}

// Verify checks an assertion and returns its content.
func (v *Verifier) Verify(ctx context.Context, token string) (Assertion, error) {
	if token == "" {
		return Assertion{}, ErrMissing
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Assertion{}, ErrMalformed
	}

	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if err := decodeSegment(parts[0], &header); err != nil {
		return Assertion{}, ErrMalformed
	}

	// ES256 only. Accepting the algorithm the token names is the classic JWT
	// vulnerability: "none" bypasses verification entirely, and an HMAC
	// algorithm lets an attacker sign with the PUBLIC key as the shared secret.
	if header.Alg != "ES256" {
		return Assertion{}, fmt.Errorf("%w: algorithm %q is not ES256", ErrSignature, header.Alg)
	}
	if header.Kid == "" {
		return Assertion{}, ErrMalformed
	}

	key, err := v.keyByID(ctx, header.Kid)
	if err != nil {
		return Assertion{}, err
	}

	if err := verifyES256(key, parts[0]+"."+parts[1], parts[2]); err != nil {
		return Assertion{}, err
	}

	var claims struct {
		Issuer   string `json:"iss"`
		Subject  string `json:"sub"`
		Email    string `json:"email"`
		Audience string `json:"aud"`
		IssuedAt int64  `json:"iat"`
		Expiry   int64  `json:"exp"`
	}
	if err := decodeSegment(parts[1], &claims); err != nil {
		return Assertion{}, ErrMalformed
	}

	if claims.Issuer != Issuer {
		return Assertion{}, fmt.Errorf("%w: %q", ErrIssuer, claims.Issuer)
	}

	// Compared exactly. A prefix or suffix match would accept an assertion
	// minted for a neighbouring backend service whose id merely shares a
	// prefix with this one.
	if claims.Audience != v.audience {
		return Assertion{}, ErrAudience
	}

	if claims.Subject == "" {
		return Assertion{}, ErrMalformed
	}

	now := v.now()
	// A small skew allowance on expiry only. None on iat: a token from the
	// future is not a clock problem worth tolerating.
	if claims.Expiry == 0 || now.After(time.Unix(claims.Expiry, 0).Add(30*time.Second)) {
		return Assertion{}, ErrExpired
	}

	return Assertion{
		Subject:   claims.Subject,
		Email:     claims.Email,
		IssuedAt:  time.Unix(claims.IssuedAt, 0),
		ExpiresAt: time.Unix(claims.Expiry, 0),
	}, nil
}

// FromRequest verifies the assertion carried by an HTTP request.
func (v *Verifier) FromRequest(r *http.Request) (Assertion, error) {
	return v.Verify(r.Context(), r.Header.Get(HeaderName))
}

func (v *Verifier) keyByID(ctx context.Context, kid string) (*ecdsa.PublicKey, error) {
	v.mu.RLock()
	key, ok := v.keys[kid]
	fresh := v.now().Sub(v.fetchedAt) < v.ttl
	v.mu.RUnlock()

	if ok && fresh {
		return key, nil
	}

	// An unknown kid triggers exactly one refetch. Google rotates these keys,
	// so a kid absent from a cached set is the expected shape of a rotation
	// rather than an attack — but refetching on every unknown kid without the
	// freshness check above would let an attacker drive unbounded outbound
	// requests by sending garbage kids.
	if err := v.refresh(ctx); err != nil {
		return nil, err
	}

	v.mu.RLock()
	key, ok = v.keys[kid]
	v.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: kid %q", ErrUnknownKey, kid)
	}
	return key, nil
}

func (v *Verifier) refresh(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.keysURL, nil)
	if err != nil {
		return fmt.Errorf("iap: build key request: %w", err)
	}
	resp, err := v.client.Do(req)
	if err != nil {
		return fmt.Errorf("iap: fetch keys: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("iap: fetch keys: status %d", resp.StatusCode)
	}

	var jwks struct {
		Keys []struct {
			Kid string `json:"kid"`
			Crv string `json:"crv"`
			X   string `json:"x"`
			Y   string `json:"y"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return fmt.Errorf("iap: decode keys: %w", err)
	}

	parsed := make(map[string]*ecdsa.PublicKey, len(jwks.Keys))
	for _, k := range jwks.Keys {
		pub, err := jwkToECDSA(k.Crv, k.X, k.Y)
		if err != nil {
			// Skip rather than fail: one unparseable key should not make every
			// other key unusable.
			continue
		}
		parsed[k.Kid] = pub
	}
	if len(parsed) == 0 {
		return errors.New("iap: key set contained no usable keys")
	}

	v.mu.Lock()
	v.keys = parsed
	v.fetchedAt = v.now()
	v.mu.Unlock()
	return nil
}

func decodeSegment(segment string, into any) error {
	raw, err := base64.RawURLEncoding.DecodeString(segment)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, into)
}

func verifyES256(key *ecdsa.PublicKey, signingInput, signature string) error {
	sig, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil {
		return ErrMalformed
	}
	// ES256 signatures are the raw concatenation of two 32-byte integers, NOT
	// the ASN.1 DER form that ecdsa.VerifyASN1 expects. Passing one to the
	// other fails every valid signature, which reads as a key mismatch.
	if len(sig) != 64 {
		return ErrSignature
	}

	digest := sha256.Sum256([]byte(signingInput))
	r := new(big.Int).SetBytes(sig[:32])
	s := new(big.Int).SetBytes(sig[32:])

	if !ecdsa.Verify(key, digest[:], r, s) {
		return ErrSignature
	}
	return nil
}

func jwkToECDSA(crv, x, y string) (*ecdsa.PublicKey, error) {
	if crv != "P-256" {
		return nil, fmt.Errorf("iap: unsupported curve %q", crv)
	}
	xb, err := base64.RawURLEncoding.DecodeString(x)
	if err != nil {
		return nil, err
	}
	yb, err := base64.RawURLEncoding.DecodeString(y)
	if err != nil {
		return nil, err
	}
	return &ecdsa.PublicKey{
		Curve: elliptic.P256(),
		X:     new(big.Int).SetBytes(xb),
		Y:     new(big.Int).SetBytes(yb),
	}, nil
}
