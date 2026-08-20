# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Validate policy inputs and compose ephemeral Conftest data."""

from __future__ import annotations

import argparse
import hashlib
import json
from datetime import UTC, datetime
from pathlib import Path
from typing import Any, NoReturn


def fail(message: str) -> NoReturn:
    raise SystemExit(f"check-plan: {message}")


def load_json(path: Path, kind: str) -> Any:
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeError, json.JSONDecodeError) as error:
        fail(f"{kind} is not readable JSON: {path}: {error}")


def nonempty_list(value: Any) -> bool:
    return isinstance(value, list) and bool(value)


def nonempty_object(value: Any) -> bool:
    return isinstance(value, dict) and bool(value)


def validate_plan(plan: Any) -> None:
    if not isinstance(plan, dict) or not isinstance(plan.get("resource_changes"), list):
        fail("plan must be Terraform plan JSON with a resource_changes array")


def validate_profile(profile: Any) -> None:
    valid = (
        isinstance(profile, dict)
        and profile.get("schema_version") == 1
        and isinstance(profile.get("profile_id"), str)
        and bool(profile["profile_id"].strip())
        and nonempty_list(profile.get("approved_locations"))
        and nonempty_list(profile.get("required_labels"))
        and nonempty_list(profile.get("data_resource_types"))
        and nonempty_object(profile.get("classifications"))
    )
    if not valid:
        fail("profile schema is invalid or a required policy collection is empty")

    for name, classification in profile["classifications"].items():
        if not (
            isinstance(name, str)
            and isinstance(classification, dict)
            and nonempty_list(classification.get("allowed_locations"))
            and nonempty_object(classification.get("retention"))
        ):
            fail(
                f"profile classification {name!r} has empty or invalid residency/retention controls"
            )


def validate_approval_document(document: Any) -> list[Any]:
    if not (
        isinstance(document, dict)
        and document.get("schema_version") == 1
        and isinstance(document.get("approvals"), list)
    ):
        fail("approval document must have schema_version 1 and an approvals array")
    return document["approvals"]


def main() -> None:
    parser = argparse.ArgumentParser(add_help=False)
    parser.add_argument("--plan", required=True, type=Path)
    parser.add_argument("--profile", required=True, type=Path)
    parser.add_argument("--approval", type=Path)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()

    plan_bytes = args.plan.read_bytes()
    plan = load_json(args.plan, "plan")
    profile = load_json(args.profile, "profile")
    validate_plan(plan)
    validate_profile(profile)

    approvals: list[Any] = []
    if args.approval is not None:
        approvals = validate_approval_document(load_json(args.approval, "approval"))

    context = {
        "policy_input": {
            "mindclade": {
                "profile": profile,
                "approvals": approvals,
                "runtime": {
                    "now": datetime.now(UTC).isoformat(timespec="seconds").replace("+00:00", "Z"),
                    "plan_digest": hashlib.sha256(plan_bytes).hexdigest(),
                },
            }
        }
    }
    args.output.write_text(
        json.dumps(context, indent=2, sort_keys=True) + "\n",
        encoding="utf-8",
    )


if __name__ == "__main__":
    main()
