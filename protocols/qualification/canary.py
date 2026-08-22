#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Run fail-closed, authenticated, read-only protobuf production canaries."""

from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import json
import os
import subprocess
import tempfile
from collections.abc import Callable, Sequence
from pathlib import Path
from urllib.parse import urlsplit

EXPECTED_SERVICES = frozenset(
    {
        "mindclade.artifact.v1.ArtifactService",
        "mindclade.data.v1.DatasetService",
        "mindclade.evaluation.v1.EvaluationService",
        "mindclade.inference.v1.InferenceService",
        "mindclade.orchestration.v1.RunService",
        "mindclade.registry.v1.ModelRegistryService",
        "mindclade.runtime.v1.RuntimeExecution",
        "mindclade.runtime.v1.RuntimePolicyFeed",
        "mindclade.runtime.v1.WorkerControl",
    }
)

READ_CANARIES = (
    "mindclade.artifact.v1.ArtifactService/ListArtifacts",
    "mindclade.data.v1.DatasetService/ListDatasets",
    "mindclade.evaluation.v1.EvaluationService/ListEvaluations",
    "mindclade.orchestration.v1.RunService/ListRuns",
    "mindclade.registry.v1.ModelRegistryService/ListModels",
)

Runner = Callable[..., subprocess.CompletedProcess[str]]


class CanaryError(RuntimeError):
    """A connected canary violated its production contract."""


def _endpoint(value: str) -> str:
    try:
        parsed = urlsplit(value)
        port = parsed.port
    except ValueError as exc:
        raise CanaryError("endpoint URL is malformed") from exc
    if (
        parsed.scheme != "https"
        or not parsed.hostname
        or parsed.username
        or parsed.password
        or parsed.query
        or parsed.fragment
        or parsed.path not in {"", "/"}
    ):
        raise CanaryError("endpoint must be an origin-only HTTPS URL")
    if port is not None and not 1 <= port <= 65535:
        raise CanaryError("endpoint port is invalid")
    return value.rstrip("/")


def _run(
    runner: Runner,
    argv: Sequence[str],
    *,
    accepted: frozenset[int] = frozenset({0}),
) -> subprocess.CompletedProcess[str]:
    result = runner(list(argv), check=False, capture_output=True, text=True)
    if result.returncode not in accepted:
        detail = (result.stderr or result.stdout or "").strip()
        raise CanaryError(
            f"canary command returned {result.returncode}, expected {sorted(accepted)}: {detail}"
        )
    return result


def run_canary(
    endpoint: str,
    token: str,
    schema: Path,
    *,
    buf: str = "buf",
    runner: Runner = subprocess.run,
) -> dict[str, object]:
    endpoint = _endpoint(endpoint)
    if not token or "\n" in token or "\r" in token:
        raise CanaryError("bearer token is absent or malformed")
    if not schema.is_dir() or not (schema / "buf.yaml").is_file():
        raise CanaryError("schema must name the repository protocols module")

    header_path: Path | None = None
    try:
        descriptor, raw_path = tempfile.mkstemp(prefix="mindclade-canary-header-")
        header_path = Path(raw_path)
        with os.fdopen(descriptor, "w", encoding="utf-8") as stream:
            stream.write(f"Authorization: Bearer {token}\n")

        common = [
            buf,
            "curl",
            "--protocol",
            "grpc",
            "--timeout",
            "15s",
            "--connect-timeout",
            "5",
        ]
        reflected = _run(
            runner,
            [
                *common,
                "--header",
                f"@{header_path}",
                "--reflect-header",
                "*",
                "--list-services",
                endpoint,
            ],
        )
        reflected_services = {
            line.strip() for line in reflected.stdout.splitlines() if line.strip()
        }
        missing = EXPECTED_SERVICES - reflected_services
        if missing:
            raise CanaryError(f"reflection is missing promoted services: {sorted(missing)}")

        # Authentication must be enforced, not merely accepted when present.
        unauthenticated = _run(
            runner,
            [
                *common,
                "--schema",
                str(schema),
                "--data",
                "{}",
                f"{endpoint}/{READ_CANARIES[0]}",
            ],
            accepted=frozenset({128}),  # gRPC UNAUTHENTICATED (16 << 3).
        )
        if "unauthenticated" not in (f"{unauthenticated.stdout}\n{unauthenticated.stderr}".lower()):
            raise CanaryError("anonymous request failed without UNAUTHENTICATED evidence")

        response_digests: dict[str, str] = {}
        for method in READ_CANARIES:
            response = _run(
                runner,
                [
                    *common,
                    "--schema",
                    str(schema),
                    "--header",
                    f"@{header_path}",
                    "--data",
                    "{}",
                    f"{endpoint}/{method}",
                ],
            )
            response_digests[method] = hashlib.sha256(response.stdout.encode("utf-8")).hexdigest()

        return {
            "schemaVersion": 1,
            "endpointOrigin": endpoint,
            "sourceSha": os.environ.get("GITHUB_SHA", "connected-local-run"),
            "qualifiedAt": dt.datetime.now(dt.UTC)
            .replace(microsecond=0)
            .isoformat()
            .replace("+00:00", "Z"),
            "transport": "grpc-over-tls",
            "authentication": {
                "authenticatedReads": "pass",
                "anonymousRead": "UNAUTHENTICATED",
            },
            "reflectedServices": sorted(EXPECTED_SERVICES),
            "readResponseSha256": response_digests,
        }
    finally:
        if header_path is not None:
            header_path.unlink(missing_ok=True)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--endpoint", required=True)
    parser.add_argument("--token-env", default="MINDCLADE_CANARY_TOKEN")
    parser.add_argument("--schema", type=Path, default=Path("protocols"))
    parser.add_argument("--buf", default="buf")
    parser.add_argument("--evidence", type=Path, required=True)
    args = parser.parse_args()
    try:
        evidence = run_canary(
            args.endpoint,
            os.environ.get(args.token_env, ""),
            args.schema,
            buf=args.buf,
        )
        args.evidence.write_text(
            json.dumps(evidence, indent=2, sort_keys=True) + "\n", encoding="utf-8"
        )
    except (CanaryError, OSError) as exc:
        print(f"ERROR: {exc}")
        return 1
    print(f"authenticated protobuf canary passed: {args.evidence}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
