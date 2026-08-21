# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

package mindclade.infrastructure.security_test

import data.mindclade.infrastructure.security
import rego.v1

valid := {
    "schemaVersion": "mindclade.dev/infrastructure-security-control/v1",
    "owner": "security-platform",
    "failurePolicy": {
        "mode": "fail-closed",
        "onMissingEvidence": "deny-activation",
    },
    "activationPolicy": {
        "enabledByDefault": false,
        "exactRevisionRequired": true,
        "qualificationDigestRequired": true,
    },
    "rollbackPolicy": {"preserveAuditTrail": true},
}

test_valid_contract_has_no_denials if {
    denials := security.deny with input as valid
    count(denials) == 0
}

test_default_activation_is_denied if {
    invalid := object.union(valid, {"activationPolicy": object.union(valid.activationPolicy, {"enabledByDefault": true})})
    denials := security.deny with input as invalid
    "SECURITY_CONTROL_DEFAULT_ACTIVE: repository contracts cannot activate an environment" in denials
}

test_fail_open_contract_is_denied if {
    invalid := object.union(valid, {"failurePolicy": object.union(valid.failurePolicy, {"mode": "fail-open"})})
    denials := security.deny with input as invalid
    "SECURITY_CONTROL_FAIL_OPEN: failure mode must be fail-closed" in denials
}
