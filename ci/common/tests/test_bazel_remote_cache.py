# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""The staged remote cache must stay inert, and must fail closed rather than half-configure."""

from __future__ import annotations

import copy
import importlib.util
import json
import re
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[3]
_spec = importlib.util.spec_from_file_location(
    "bazel_remote_cache", ROOT / "ci/common/bazel_remote_cache.py"
)
assert _spec is not None and _spec.loader is not None
remote = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(remote)

ENDPOINT = "https://storage.googleapis.com/mindclade-bazel-cache-ci"


def checked_in() -> dict:
    return remote.load_contract()


def activated() -> dict:
    contract = copy.deepcopy(checked_in())
    contract["activation"] = {"enabled": True, "reason": "qualified"}
    return contract


# --- staged state -------------------------------------------------------------------------


def test_checked_in_contract_is_disabled() -> None:
    """The committed contract must never be the activated one."""
    assert checked_in()["activation"]["enabled"] is False


@pytest.mark.parametrize("role", ("reader", "writer"))
def test_disabled_contract_emits_nothing(role: str) -> None:
    """Inert means no trace: `--remote_cache=` in user.bazelrc must mean the cache is really on."""
    assert remote.remote_lines(checked_in(), role=role) == ""


def test_endpoint_without_activation_is_refused_not_ignored() -> None:
    """The two halves of the decision diverging is a stop, not a reason to use the trusted half."""
    with pytest.raises(remote.RemoteCacheError, match="while activation is disabled"):
        remote.remote_lines(checked_in(), role="writer", endpoint=ENDPOINT)


def test_activation_without_endpoint_is_refused() -> None:
    with pytest.raises(remote.RemoteCacheError, match="requires an endpoint"):
        remote.remote_lines(activated(), role="writer")


# --- activated behaviour ------------------------------------------------------------------


def test_writer_uploads_and_reader_does_not() -> None:
    """The same role select_trust derives for the disk cache governs the shared cache."""
    writer = remote.remote_lines(activated(), role="writer", endpoint=ENDPOINT)
    reader = remote.remote_lines(activated(), role="reader", endpoint=ENDPOINT)
    assert f"build --remote_cache={ENDPOINT}\n" in writer
    assert "build --google_default_credentials\n" in writer
    assert "build --remote_upload_local_results=true\n" in writer
    assert "build --remote_upload_local_results=false\n" in reader
    assert "--remote_cache=" in reader


def test_unknown_role_is_refused() -> None:
    with pytest.raises(remote.RemoteCacheError, match="invalid Bazel cache role"):
        remote.remote_lines(activated(), role="admin", endpoint=ENDPOINT)


# --- endpoint validation ------------------------------------------------------------------


@pytest.mark.parametrize(
    "endpoint",
    (
        "http://storage.googleapis.com/bucket",  # not https
        "https://storage.googleapis.com/",  # no bucket
        "https://storage.googleapis.com",  # no path at all
        "https://attacker.test/bucket",  # wrong host
        "https://user:pass@storage.googleapis.com/bucket",  # embedded credentials
        "https://storage.googleapis.com/bucket?x=1",  # query
        "https://storage.googleapis.com/bucket#f",  # fragment
        "https://storage.googleapis.com/bucket/prefix",  # not a bare bucket
    ),
)
def test_hostile_endpoints_are_refused(endpoint: str) -> None:
    with pytest.raises(remote.RemoteCacheError, match=re.escape("storage.googleapis.com")):
        remote.remote_lines(activated(), role="writer", endpoint=endpoint)


def test_trailing_slash_is_normalized() -> None:
    lines = remote.remote_lines(activated(), role="writer", endpoint=ENDPOINT + "/")
    assert f"build --remote_cache={ENDPOINT}\n" in lines


# --- contract drift -----------------------------------------------------------------------


@pytest.mark.parametrize(
    ("mutate", "match"),
    (
        (lambda c: c.pop("bucket_output"), "field inventory is not exact"),
        (lambda c: c.update(schema_version=2), "contract v1"),
        (lambda c: c.update(terraform_module="infra/terraform/modules/other"), "module identity"),
        (lambda c: c.update(bucket_output="gs_uri"), "endpoint output identity"),
        (lambda c: c.update(trusted_writer_events=["pull_request"]), "writer events"),
        (lambda c: c.update(activation={"enabled": "yes", "reason": "x"}), "must be boolean"),
        (lambda c: c.update(activation={"enabled": False}), "activation inventory"),
    ),
)
def test_contract_drift_fails_closed(mutate, match: str, tmp_path: Path) -> None:
    contract = checked_in()
    mutate(contract)
    path = tmp_path / "contract.json"
    path.write_text(json.dumps(contract), encoding="utf-8")
    with pytest.raises(remote.RemoteCacheError, match=match):
        remote.load_contract(path)
