// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package auth

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"mindclade.internal/libs/go/faults"
)

const (
	MaximumAttributes     = 64
	MaximumAttributeKey   = 64
	MaximumAttributeValue = 512
)

type attributeProfile struct {
	sentinel  error
	code      faults.Code
	message   string
	reason    string
	operation string
}

var (
	principalAttributeProfile = attributeProfile{
		sentinel:  ErrInvalidPrincipal,
		code:      faults.CodeUnauthenticated,
		message:   "invalid authenticated principal",
		reason:    "invalid_principal_attribute",
		operation: "auth.Principal.Validate",
	}
	credentialAttributeProfile = attributeProfile{
		sentinel:  ErrInvalidCredential,
		code:      faults.CodeUnauthenticated,
		message:   "invalid authentication credential",
		reason:    "invalid_credential_attribute",
		operation: "auth.Credential.Validate",
	}
	claimsAttributeProfile = attributeProfile{
		sentinel:  ErrInvalidClaims,
		code:      faults.CodeUnauthenticated,
		message:   "invalid authentication claims",
		reason:    "invalid_claim_attribute",
		operation: "auth.Claims.Validate",
	}
	resourceAttributeProfile = attributeProfile{
		sentinel:  ErrInvalidResource,
		code:      faults.CodeInvalidArgument,
		message:   "invalid authorization resource",
		reason:    "invalid_resource_attribute",
		operation: "auth.Resource.Validate",
	}
	decisionAttributeProfile = attributeProfile{
		sentinel:  ErrInvalidDecision,
		code:      faults.CodeInternal,
		message:   "invalid authorization decision",
		reason:    "invalid_decision_obligation",
		operation: "auth.Decision.Validate",
	}
)

func cloneAttributes(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func normalizePrincipalAttributes(input map[string]string) (map[string]string, error) {
	return normalizeAttributes(input, principalAttributeProfile)
}

func normalizeCredentialAttributes(input map[string]string) (map[string]string, error) {
	return normalizeAttributes(input, credentialAttributeProfile)
}

func normalizeClaimsAttributes(input map[string]string) (map[string]string, error) {
	return normalizeAttributes(input, claimsAttributeProfile)
}

func normalizeResourceAttributes(input map[string]string) (map[string]string, error) {
	return normalizeAttributes(input, resourceAttributeProfile)
}

func normalizeDecisionObligations(input map[string]string) (map[string]string, error) {
	return normalizeAttributes(input, decisionAttributeProfile)
}

func normalizeAttributes(input map[string]string, profile attributeProfile) (map[string]string, error) {
	if len(input) == 0 {
		return nil, nil
	}
	if len(input) > MaximumAttributes {
		return nil, attributeFault(
			profile,
			errors.New("too many attributes"),
			faults.Fields{"attribute_count": len(input)},
		)
	}

	output := make(map[string]string, len(input))
	for rawKey, rawValue := range input {
		key := strings.TrimSpace(rawKey)
		value := strings.TrimSpace(rawValue)
		if !validAttributeKey(key) ||
			len(value) > MaximumAttributeValue ||
			!utf8.ValidString(value) ||
			sensitiveAttributeKey(key) ||
			containsControl(value) {
			return nil, attributeFault(
				profile,
				errors.New("invalid attribute"),
				faults.Fields{"attribute_key": key, "value_length": len(value)},
			)
		}
		if _, exists := output[key]; exists {
			return nil, attributeFault(
				profile,
				errors.New("duplicate normalized attribute"),
				faults.Fields{"attribute_key": key},
			)
		}
		output[key] = value
	}
	return output, nil
}

func attributeFault(profile attributeProfile, cause error, fields faults.Fields) error {
	return newFault(
		errors.Join(profile.sentinel, cause),
		profile.code,
		profile.message,
		profile.reason,
		profile.operation,
		fields,
	)
}

func validAttributeKey(value string) bool {
	if value == "" || len(value) > MaximumAttributeKey {
		return false
	}
	previousSeparator := false
	for index := 0; index < len(value); index++ {
		character := value[index]
		isLetter := character >= 'a' && character <= 'z'
		isDigit := character >= '0' && character <= '9'
		isSeparator := character == '_' || character == '-' || character == '.'
		if !isLetter && !isDigit && !isSeparator ||
			index == 0 && !isLetter ||
			index == len(value)-1 && isSeparator ||
			isSeparator && previousSeparator {
			return false
		}
		previousSeparator = isSeparator
	}
	return true
}

func sensitiveAttributeKey(value string) bool {
	canonical := strings.NewReplacer("-", "_", ".", "_", ":", "_", "/", "_").Replace(strings.ToLower(value))
	for _, marker := range []string{
		"password",
		"secret",
		"token",
		"api_key",
		"private_key",
		"credential",
		"authorization",
		"cookie",
	} {
		if strings.Contains(canonical, marker) {
			return true
		}
	}
	return false
}

func containsControl(value string) bool {
	for _, character := range value {
		if unicode.IsControl(character) {
			return true
		}
	}
	return false
}

func validIdentityText(value string, maximum int) bool {
	return value != "" && len(value) <= maximum && utf8.ValidString(value) && !containsControl(value)
}
