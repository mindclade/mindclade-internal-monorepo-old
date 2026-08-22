# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Security regressions for repository-home validation."""

from tools.docs import validate_repository_home


def test_shields_reference_requires_exact_hostname() -> None:
    assert validate_repository_home.has_remote_shields_reference(
        "![build](https://img.shields.io/badge/build-passing.svg)"
    )
    assert validate_repository_home.has_remote_shields_reference(
        "![build](https://IMG.SHIELDS.IO/badge/build-passing.svg)"
    )
    assert not validate_repository_home.has_remote_shields_reference(
        "![build](https://img.shields.io.evil.example/badge/build-passing.svg)"
    )
    assert not validate_repository_home.has_remote_shields_reference(
        "[redirect](https://example.test/?next=https://img.shields.io/badge/build-passing.svg)"
    )
