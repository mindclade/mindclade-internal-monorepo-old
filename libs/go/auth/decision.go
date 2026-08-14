// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package auth

import (
	"strings"

	"mindclade.internal/libs/go/faults"
)

type Effect string

const (
	EffectAllow   Effect = "allow"
	EffectDeny    Effect = "deny"
	EffectAbstain Effect = "abstain"
)

func (effect Effect) Valid() bool {
	return effect == EffectAllow || effect == EffectDeny || effect == EffectAbstain
}

type Decision struct {
	effect      Effect
	reason      string
	policyID    string
	obligations map[string]string
}

func Allow(reason string) Decision {
	return Decision{effect: EffectAllow, reason: strings.TrimSpace(reason)}
}
func Deny(reason string) Decision {
	return Decision{effect: EffectDeny, reason: strings.TrimSpace(reason)}
}
func Abstain(reason string) Decision {
	return Decision{effect: EffectAbstain, reason: strings.TrimSpace(reason)}
}

func NewDecision(effect Effect, reason, policyID string, obligations map[string]string) (Decision, error) {
	normalized, err := normalizeDecisionObligations(obligations)
	if err != nil {
		return Decision{}, err
	}
	decision := Decision{effect: effect, reason: strings.TrimSpace(reason), policyID: strings.TrimSpace(policyID), obligations: normalized}
	if err := decision.Validate(); err != nil {
		return Decision{}, err
	}
	return decision, nil
}

func (decision Decision) Effect() Effect   { return decision.effect }
func (decision Decision) Reason() string   { return decision.reason }
func (decision Decision) PolicyID() string { return decision.policyID }
func (decision Decision) Obligations() map[string]string {
	return cloneAttributes(decision.obligations)
}
func (decision Decision) Allowed() bool { return decision.effect == EffectAllow }

func (decision Decision) Validate() error {
	reasonRequired := decision.effect != EffectAllow
	if !decision.effect.Valid() || !validDecisionReason(decision.reason, reasonRequired) ||
		(decision.policyID != "" && !validIdentityText(decision.policyID, 256)) {
		return newFault(ErrInvalidDecision, faults.CodeInternal, "invalid authorization decision", "invalid_authorization_decision", "auth.Decision.Validate", nil)
	}
	if _, err := normalizeDecisionObligations(decision.obligations); err != nil {
		return err
	}
	return nil
}

func validDecisionReason(value string, required bool) bool {
	if value == "" {
		return !required
	}
	if len(value) > 256 || value != strings.ToLower(value) {
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
