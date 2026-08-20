# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from libs.python.observability import REDACTED, redact, redact_fields


def test_redacts_nested_credentials_and_url_queries() -> None:
    value = {
        "authorization": "Bearer secret",
        "nested": {
            "api_key": "secret",
            "download": "https://user:pass@example.com/file?signature=secret#fragment",
        },
    }

    assert redact(value) == {
        "authorization": REDACTED,
        "nested": {
            "api_key": REDACTED,
            "download": "https://example.com/file",
        },
    }


def test_redaction_snapshot_does_not_alias_input() -> None:
    source: dict[str, object] = {"labels": ["safe"]}
    snapshot = redact_fields(source)
    source["labels"] = ["changed"]

    assert snapshot["labels"] == ["safe"]
