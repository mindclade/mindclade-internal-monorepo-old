# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from test_execution_ticket_golden import claims


def test_ticket_claim_encoding_is_versioned_and_bounded():
    encoded = claims()
    assert encoded.startswith(b"MCCE1/execution-ticket-claims\0")
    assert len(encoded) < 64 * 1024
