// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package signing

import "strings"

const (
	// MaximumDomainLength bounds the purpose label. It matches the document-type
	// bound already enforced by the canonical claim encoders in control/, so a
	// label that is legal there is legal here.
	MaximumDomainLength = 128

	// MaximumPreimageBytes bounds the buffer Preimage builds. Signed payloads in
	// this repository are claim documents and page tokens — kilobytes, not
	// megabytes — so a 1 MiB ceiling is far above every real caller while still
	// refusing to allocate an unbounded buffer for a hostile length.
	MaximumPreimageBytes = 1 << 20

	// domainPrefix and domainTerminator reproduce the framing that
	// control/runtime_authority/canonical.go and control/evidence/canonical.go
	// already emit ("MCCE1/" + document type + NUL), so a domain-separated
	// signature produced here is byte-compatible with the canonical claim
	// encoding those packages share with libs/rust/worker_protocol.
	domainPrefix     = "MCCE1/"
	domainTerminator = 0x00
)

// Domain is the purpose a signature is valid for, and nothing else.
//
// Why this type exists: HMAC and Ed25519 both sign whatever bytes they are
// handed, so a signature is only ever a statement about a byte string — never
// about what that byte string was *for*. When one key signs several kinds of
// document (this repository shares a single control-plane HMAC key across page
// tokens, execution tickets, admission grants, route snapshots, revocation
// snapshots, and evidence claims), a signature minted for the cheapest of them
// is structurally a valid signature for the most privileged one wherever the
// encodings can be made to collide. Committing the purpose to the signed bytes
// is what makes that collision impossible rather than merely unlikely.
//
// Every claim encoder in control/ already does this by prefixing its document
// type. Page tokens did not, which is the gap this type closes; see
// libs/go/pagination.
type Domain string

// ParseDomain validates a purpose label. Labels are lowercase and versioned by
// convention ("pagination-cursor/v1") so that a deliberate format change can be
// rolled out as a new domain rather than as a silent reinterpretation of the
// same one.
//
// A NUL is rejected because NUL terminates the label inside the preimage: if a
// caller could embed one, two different domains could frame identically and the
// separation would be defeated by the very byte that is supposed to enforce it.
func ParseDomain(value string) (Domain, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || len(trimmed) > MaximumDomainLength || trimmed != value {
		return "", invalid(ErrInvalidDomain, "invalid signing domain", "invalid_signing_domain", "signing.ParseDomain", map[string]any{"domain": value})
	}
	for index, character := range trimmed {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' {
			continue
		}
		if index > 0 && (character == '-' || character == '_' || character == '.' || character == '/') {
			continue
		}
		return "", invalid(ErrInvalidDomain, "invalid signing domain", "invalid_signing_domain", "signing.ParseDomain", map[string]any{"domain": value})
	}
	return Domain(trimmed), nil
}

// MustParseDomain is for package-level domain constants, where an invalid label
// is a programming error that must not reach a running process.
func MustParseDomain(value string) Domain {
	domain, err := ParseDomain(value)
	if err != nil {
		panic(err)
	}
	return domain
}

func (domain Domain) String() string { return string(domain) }
func (domain Domain) Valid() bool {
	parsed, err := ParseDomain(string(domain))
	return err == nil && parsed == domain
}

// Preimage returns the exact bytes to hand to Signer.Sign or Verifier.Verify
// for a payload signed under domain.
//
// The encoding is "MCCE1/" + domain + NUL + payload. It is injective because
// the domain is NUL-free and NUL-terminated: the terminator can only be the end
// of the label, so no payload can shift bytes across the boundary to imitate a
// different domain. That is the same reasoning as the trailing NUL in
// services/artifact_proxy/src/signing.rs.
//
// A caller that does not route through Preimage gets no separation at all, so
// prefer a constructor that takes a Domain and calls this internally over
// leaving it to each call site to remember.
func Preimage(domain Domain, payload []byte) ([]byte, error) {
	if !domain.Valid() {
		return nil, invalid(ErrInvalidDomain, "invalid signing domain", "invalid_signing_domain", "signing.Preimage", map[string]any{"domain": domain.String()})
	}
	framing := len(domainPrefix) + len(domain) + 1
	if len(payload) > MaximumPreimageBytes-framing {
		return nil, invalid(ErrInvalidDomain, "signing payload exceeds the bounded preimage", "signing_payload_too_large", "signing.Preimage", map[string]any{"domain": domain.String(), "payload_bytes": len(payload), "maximum_bytes": MaximumPreimageBytes})
	}
	preimage := make([]byte, 0, framing+len(payload))
	preimage = append(preimage, domainPrefix...)
	preimage = append(preimage, domain...)
	preimage = append(preimage, domainTerminator)
	preimage = append(preimage, payload...)
	return preimage, nil
}
