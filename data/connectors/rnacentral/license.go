// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package rnacentral

import "errors"

type LicensePolicy struct {
	Reference    string
	ApprovedUses []string
}

func (p LicensePolicy) ValidateUse(use string) error {
	if p.Reference == "" || use == "" || len(p.ApprovedUses) == 0 {
		return errors.New("rnacentral license policy is incomplete")
	}
	for _, approved := range p.ApprovedUses {
		if approved == use {
			return nil
		}
	}
	return errors.New("rnacentral license policy does not approve requested use")
}
