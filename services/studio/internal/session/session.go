// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

// Package session implements __Host-mc_session: a short-lived, AEAD-encrypted
// cookie that caches the BFF's authorization resolution for one IAP subject.
//
// # The cookie is not a bearer of identity
//
// That reframing is the whole reason it is safe to hold client-side. IAP proves
// identity on every request and injects a signed assertion; this cookie only
// caches what the BFF decided about that identity. It is bound to the subject,
// so replaying it requires a valid IAP assertion for the same subject — which
// an attacker holding only a stolen cookie does not have.
//
// A stolen or forged cookie is therefore INERT on its own. That property is a
// direct consequence of keeping IAP in front, and it does not survive dropping
// it: if IAP is ever removed, sessions must become server-side in the same
// change. The coupling runs in that direction only.
//
// # Two keys, always
//
// Both keys are live: either may decrypt, the newer always encrypts. Rotating a
// single key logs every user out at the same instant, which presents as a total
// outage. The overlap must exceed twice the TTL — with five minutes, a 30-day
// rotation is enormous margin and costs nothing.
//
// # What it deliberately gives up
//
// No instant global logout. The floor is the TTL: IAM is the revocation point,
// IAP stops asserting, and the cache expires within five minutes. If a "revoke
// everyone now" button ever becomes a requirement, it comes back as a small
// deny-list — but that is not built speculatively.
package session

import (
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"golang.org/x/crypto/chacha20poly1305"
)

// CookieName is fixed by the __Host- prefix rules, which are enforced by the
// browser rather than by us: a __Host- cookie MUST be Secure, MUST have
// Path=/, and MUST NOT carry a Domain attribute. The last is the useful one —
// it makes widening the cookie's scope to a parent domain impossible later,
// even by accident.
const CookieName = "__Host-mc_session"

// TTL is the revocation bound, not a comfort setting. Every minute added here
// is a minute a revoked principal keeps working.
const TTL = 5 * time.Minute

var (
	ErrNoKeys        = errors.New("session: no keys configured")
	ErrMalformed     = errors.New("session: malformed cookie")
	ErrUndecryptable = errors.New("session: no key could decrypt")
	ErrExpired       = errors.New("session: expired")
	ErrSubject       = errors.New("session: subject does not match the IAP assertion")
)

// Claims is what the cookie carries. It stays SMALL AND DERIVED on purpose.
//
// Not a materialized permission set: if the authorization answer does not fit
// comfortably here, re-resolve it rather than enlarging the cookie. A cookie
// large enough to hold a permission set is also a cookie stale enough to hold
// a revoked one.
type Claims struct {
	// Subject is the IAP subject this session is bound to. Every request
	// re-checks it against the live assertion — that check is what makes a
	// stolen cookie inert.
	Subject string `json:"sub"`

	// SessionID correlates log lines across a session. Opaque, and carries no
	// authority of its own.
	SessionID string `json:"sid"`

	// AuthzVersion lets a change in the authorization model invalidate cached
	// decisions without waiting for the TTL. Bump it and every existing cookie
	// stops being accepted.
	AuthzVersion int `json:"av"`

	IssuedAt  time.Time `json:"iat"`
	ExpiresAt time.Time `json:"exp"`
}

// Key is one AEAD key. Both live keys are held at once.
type Key struct {
	// ID travels in the clear, as the first segment of the cookie, so that
	// decryption tries the right key first rather than every key in turn.
	// Naming which key sealed a value discloses nothing.
	ID   string
	aead cipher.AEAD
}

// NewKey builds a Key from 32 bytes of secret material.
func NewKey(id string, material []byte) (Key, error) {
	if len(material) != chacha20poly1305.KeySize {
		return Key{}, fmt.Errorf("session: key %q must be %d bytes, got %d",
			id, chacha20poly1305.KeySize, len(material))
	}
	// XChaCha20-Poly1305 rather than the plain variant: its 24-byte nonce is
	// large enough that random generation has no practical collision risk, so
	// there is no counter to persist and no coordination between replicas.
	aead, err := chacha20poly1305.NewX(material)
	if err != nil {
		return Key{}, fmt.Errorf("session: key %q: %w", id, err)
	}
	return Key{ID: id, aead: aead}, nil
}

// Codec seals and opens session cookies.
type Codec struct {
	// current encrypts. Both current and previous decrypt.
	current  Key
	previous *Key

	authzVersion int
	now          func() time.Time
}

// NewCodec returns a Codec. `previous` may be nil only during initial
// bootstrap; a steady-state deployment always has two live keys, because
// retrofitting rotation onto a single-key deployment means either an outage or
// a fleet-wide invalidation.
func NewCodec(current Key, previous *Key, authzVersion int) (*Codec, error) {
	if current.aead == nil {
		return nil, ErrNoKeys
	}
	if previous != nil && previous.ID == current.ID {
		return nil, fmt.Errorf("session: current and previous keys share id %q; rotation would be a no-op", current.ID)
	}
	return &Codec{
		current:      current,
		previous:     previous,
		authzVersion: authzVersion,
		now:          time.Now,
	}, nil
}

// Seal issues a cookie value for a subject.
func (c *Codec) Seal(subject, sessionID string) (string, error) {
	if subject == "" {
		return "", fmt.Errorf("session: refusing to seal an empty subject")
	}

	now := c.now().UTC()
	claims := Claims{
		Subject:      subject,
		SessionID:    sessionID,
		AuthzVersion: c.authzVersion,
		IssuedAt:     now,
		ExpiresAt:    now.Add(TTL),
	}

	plaintext, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("session: marshal claims: %w", err)
	}

	nonce := make([]byte, chacha20poly1305.NonceSizeX)
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("session: nonce: %w", err)
	}

	// The key id is additional authenticated data as well as a prefix, so an
	// attacker cannot relabel a ciphertext as having been sealed by the other
	// key and get it silently accepted.
	sealed := c.current.aead.Seal(nil, nonce, plaintext, []byte(c.current.ID))

	return c.current.ID + "." +
		base64.RawURLEncoding.EncodeToString(nonce) + "." +
		base64.RawURLEncoding.EncodeToString(sealed), nil
}

// Open decrypts and validates a cookie against the subject from the live IAP
// assertion.
//
// THE iapSubject ARGUMENT IS NOT OPTIONAL and must come from a VERIFIED
// assertion — signature, issuer, and audience all checked — never from the raw
// header. Passing an unverified value turns this from a binding check into a
// comparison of two attacker-controlled strings.
func (c *Codec) Open(value, iapSubject string) (Claims, error) {
	if iapSubject == "" {
		return Claims{}, ErrSubject
	}

	keyID, nonce, ciphertext, err := split(value)
	if err != nil {
		return Claims{}, err
	}

	key, ok := c.keyByID(keyID)
	if !ok {
		// Names a retired key: the client's session was sealed before the last
		// rotation completed. Indistinguishable from expiry to the user, and
		// handled the same way.
		return Claims{}, ErrUndecryptable
	}

	plaintext, err := key.aead.Open(nil, nonce, ciphertext, []byte(key.ID))
	if err != nil {
		return Claims{}, ErrUndecryptable
	}

	var claims Claims
	if err := json.Unmarshal(plaintext, &claims); err != nil {
		return Claims{}, ErrMalformed
	}

	if c.now().UTC().After(claims.ExpiresAt) {
		return Claims{}, ErrExpired
	}

	// An authorization-model change invalidates cached decisions immediately
	// rather than waiting out the TTL.
	if claims.AuthzVersion != c.authzVersion {
		return Claims{}, ErrExpired
	}

	// THE CHECK THAT MAKES A CLIENT-HELD SESSION SAFE.
	//
	// A valid cookie for principal A, replayed under principal B's IAP
	// assertion, fails here. Without it the cookie is a bearer token and
	// everything above is decoration.
	if claims.Subject != iapSubject {
		return Claims{}, ErrSubject
	}

	return claims, nil
}

func (c *Codec) keyByID(id string) (Key, bool) {
	if id == c.current.ID {
		return c.current, true
	}
	if c.previous != nil && id == c.previous.ID {
		return *c.previous, true
	}
	return Key{}, false
}

func split(value string) (keyID string, nonce, ciphertext []byte, err error) {
	var first, second int
	if first = indexByte(value, '.'); first < 0 {
		return "", nil, nil, ErrMalformed
	}
	rest := value[first+1:]
	if second = indexByte(rest, '.'); second < 0 {
		return "", nil, nil, ErrMalformed
	}

	keyID = value[:first]
	if keyID == "" {
		return "", nil, nil, ErrMalformed
	}

	if nonce, err = base64.RawURLEncoding.DecodeString(rest[:second]); err != nil {
		return "", nil, nil, ErrMalformed
	}
	if len(nonce) != chacha20poly1305.NonceSizeX {
		return "", nil, nil, ErrMalformed
	}
	if ciphertext, err = base64.RawURLEncoding.DecodeString(rest[second+1:]); err != nil {
		return "", nil, nil, ErrMalformed
	}
	return keyID, nonce, ciphertext, nil
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}
