// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

// Package authz owns Studio's application authorization policy.
//
// IAP proves who reached the service; it does not decide who may use Studio.
// This package deliberately authorizes only exact, stable IAP subjects. Email
// addresses are display metadata and are never an authorization input because
// an address can be reassigned to another person.
package authz

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"go.mindclade.dev/services/studio/internal/iap"
)

var (
	// ErrNoSubjects is returned during construction. An empty policy is a
	// deployment error rather than an allow-all policy.
	ErrNoSubjects = errors.New("studio authorization: no subjects configured")
	// ErrDenied is intentionally stable and contains no identity metadata. The
	// caller may log the already-verified subject, but responses stay generic.
	ErrDenied = errors.New("studio authorization: access denied")
)

const subjectPrefix = "accounts.google.com:"

// Policy is an immutable exact-subject allowlist. It is safe for concurrent
// use by every HTTP request in a process.
type Policy struct {
	subjects map[string]struct{}
}

// New parses a comma-separated list of stable IAP subjects.
//
// Duplicate or malformed subjects are rejected rather than normalized. This
// makes the deployment secret auditable: every configured grant has one
// canonical spelling, and a typo cannot silently create a policy that will
// never match.
func New(raw string) (*Policy, error) {
	values := strings.Split(raw, ",")
	subjects := make(map[string]struct{}, len(values))
	for _, value := range values {
		subject := strings.TrimSpace(value)
		if subject == "" {
			continue
		}
		if !validSubject(subject) {
			return nil, fmt.Errorf("studio authorization: invalid IAP subject %q", subject)
		}
		if _, exists := subjects[subject]; exists {
			return nil, fmt.Errorf("studio authorization: duplicate IAP subject %q", subject)
		}
		subjects[subject] = struct{}{}
	}
	if len(subjects) == 0 {
		return nil, ErrNoSubjects
	}
	return &Policy{subjects: subjects}, nil
}

// Resolve implements Studio's authorization resolver. Only Subject is read;
// Email is never consulted.
func (policy *Policy) Resolve(ctx context.Context, assertion iap.Assertion) error {
	if ctx == nil {
		return ErrDenied
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if policy == nil || len(policy.subjects) == 0 || !validSubject(assertion.Subject) {
		return ErrDenied
	}
	if _, allowed := policy.subjects[assertion.Subject]; !allowed {
		return ErrDenied
	}
	return nil
}

func validSubject(subject string) bool {
	numeric := strings.TrimPrefix(subject, subjectPrefix)
	if numeric == "" || numeric == subject {
		return false
	}
	for _, character := range numeric {
		if !unicode.IsDigit(character) || character > unicode.MaxASCII {
			return false
		}
	}
	return true
}
