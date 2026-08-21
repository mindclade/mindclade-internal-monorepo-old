# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

package mindclade.infrastructure.security

import rego.v1

deny contains "SECURITY_CONTROL_SCHEMA_INVALID: unrecognized schema version" if {
    input.schemaVersion != "mindclade.dev/infrastructure-security-control/v1"
}

deny contains "SECURITY_CONTROL_OWNER_INVALID: security-platform ownership is required" if {
    input.owner != "security-platform"
}

deny contains "SECURITY_CONTROL_FAIL_OPEN: failure mode must be fail-closed" if {
    input.failurePolicy.mode != "fail-closed"
}

deny contains "SECURITY_CONTROL_MISSING_EVIDENCE_ALLOWED: missing evidence must deny activation" if {
    input.failurePolicy.onMissingEvidence != "deny-activation"
}

deny contains "SECURITY_CONTROL_DEFAULT_ACTIVE: repository contracts cannot activate an environment" if {
    input.activationPolicy.enabledByDefault
}

deny contains "SECURITY_CONTROL_REVISION_MUTABLE: activation requires an exact revision" if {
    input.schemaVersion == "mindclade.dev/infrastructure-security-control/v1"
    not input.activationPolicy.exactRevisionRequired
}

deny contains "SECURITY_CONTROL_EVIDENCE_MUTABLE: activation requires a qualification digest" if {
    input.schemaVersion == "mindclade.dev/infrastructure-security-control/v1"
    not input.activationPolicy.qualificationDigestRequired
}

deny contains "SECURITY_CONTROL_ROLLBACK_UNAUDITED: rollback must preserve the audit trail" if {
    input.schemaVersion == "mindclade.dev/infrastructure-security-control/v1"
    not input.rollbackPolicy.preserveAuditTrail
}
