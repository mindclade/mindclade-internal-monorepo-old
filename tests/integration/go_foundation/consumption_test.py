# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Repository-level enforcement for Go foundation consumption."""

from __future__ import annotations

import re
from pathlib import Path

REPO = Path(__file__).resolve().parents[3]

COMMAND_ROLES = {
    "admin": "RoleAdmin",
    "api": "RoleAPI",
    "controller": "RoleController",
    "event_dispatcher": "RoleEventDispatcher",
    "event_projector": "RoleEventProjector",
    "ingestion_controller": "RoleIngestionController",
    "operator": "RoleOperator",
    "registry": "RoleRegistry",
    "scheduler": "RoleScheduler",
    "webhook_dispatcher": "RoleWebhookDispatcher",
}

FOUNDATION_PACKAGES = {
    "libs/go/storage/sql/transaction",
    "libs/go/storage/sql/postgres",
    "libs/go/storage/sql/migrate",
    "libs/go/signing",
    "libs/go/resourceversion",
    "libs/go/pagination",
    "libs/go/messaging",
    "libs/go/httpx/outbound",
    "libs/go/config",
    "libs/go/audit",
    "libs/go/auth",
    "libs/go/clock",
    "libs/go/connectx",
    "libs/go/coordination/cursor",
    "libs/go/coordination/inbox",
    "libs/go/coordination/leadership",
    "libs/go/coordination/outbox",
    "libs/go/coordination/projector",
    "libs/go/coordination/workqueue",
    "libs/go/faults",
    "libs/go/grpcx",
    "libs/go/httpx",
    "libs/go/idempotency",
    "libs/go/identifiers",
    "libs/go/kubernetes",
    "libs/go/observability",
    "libs/go/requestmeta",
    "libs/go/retry",
    "libs/go/servicekit",
    "libs/go/servicekit/production",
    "libs/go/storage/blob",
    "libs/go/storage/cache",
    "libs/go/storage/lease",
    "libs/go/storage/sql",
}


def test_every_control_plane_command_uses_standard_bootstrap() -> None:
    command_root = REPO / "services/control_plane/cmd"
    assert {path.name for path in command_root.iterdir() if path.is_dir()} == set(COMMAND_ROLES)

    for command, role in COMMAND_ROLES.items():
        source = (command_root / command / "main.go").read_text()
        assert '"go.mindclade.dev/services/control_plane/internal/bootstrap"' in source
        assert "bootstrap.Main(" in source
        assert f"bootstrap.{role}" in source
        assert "bootstrap.UnconfiguredFactory(" in source
        assert "servicekit.New(" not in source
        assert "signal.Notify" not in source

        build = (command_root / command / "BUILD.bazel").read_text()
        assert 'load("@rules_go//go:def.bzl", "go_binary")' in build
        assert "//services/control_plane/internal/bootstrap" in build


def test_bootstrap_delegates_to_production_builder() -> None:
    source = (REPO / "services/control_plane/internal/bootstrap/build.go").read_text()
    assert "production.NewBuilder(" in source
    assert "runtime.Dependencies.Register(builder)" in source
    assert "runtime.Components.Register(builder)" in source
    assert "servicekit.New(" not in source
    assert "servicekit.NewAssembly(" not in source


def test_consumption_matrix_covers_public_foundation() -> None:
    source = (REPO / "services/control_plane/internal/bootstrap/consumption.go").read_text()
    recorded = set(re.findall(r'"(libs/go/[^" ]+)"', source))
    missing = sorted(FOUNDATION_PACKAGES - recorded)
    assert not missing, f"foundation packages not assigned to any process role: {missing}"


def test_no_generic_go_dumping_ground_is_introduced() -> None:
    forbidden = {"common", "helpers", "shared", "utils"}
    offenders = []
    for directory in (REPO / "libs/go").rglob("*"):
        if directory.is_dir() and directory.name in forbidden:
            offenders.append(str(directory.relative_to(REPO)))
    assert not offenders, f"generic Go package boundaries are prohibited: {offenders}"
