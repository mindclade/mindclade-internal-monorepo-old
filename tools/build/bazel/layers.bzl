# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#
"""Repository-wide Bazel dependency-layer declarations.

Keep this file data-only above the macro. tools/analysis/check_bazel_layers.py reads the
literal assignments so the package groups used by BUILD files and the CI graph policy have
one source of truth.
"""

BAZEL_PACKAGE_GROUPS = {
    "boundary_apps": ["//apps/..."],
    "boundary_infra": ["//infra/..."],
    "boundary_research_experiments": ["//research/experiments/..."],
    "boundary_services": ["//services/..."],
    "boundary_training": ["//training/..."],
    "layer_foundation": [
        "//configs/...",
        "//libs/...",
        "//protocols/...",
        "//sdk/...",
    ],
    "layer_offline": [
        "//data/...",
        "//evaluation/...",
        "//kernels/...",
        "//models/...",
        "//preprocessing/...",
        "//training/...",
    ],
    "layer_platform": [
        "//architecture/...",
        "//ci/...",
        "//docs/...",
        "//examples/...",
        "//infra/...",
        "//qualification/...",
        "//security/...",
        "//tests/...",
        "//tools/...",
    ],
    "layer_research": ["//research/..."],
    "layer_serving": [
        "//apps/...",
        "//control/...",
        "//services/...",
        "//serving/...",
    ],
    "non_research": [
        "//apps/...",
        "//architecture/...",
        "//ci/...",
        "//configs/...",
        "//control/...",
        "//data/...",
        "//docs/...",
        "//evaluation/...",
        "//examples/...",
        "//infra/...",
        "//kernels/...",
        "//libs/...",
        "//models/...",
        "//preprocessing/...",
        "//protocols/...",
        "//qualification/...",
        "//sdk/...",
        "//security/...",
        "//services/...",
        "//serving/...",
        "//tests/...",
        "//tools/...",
        "//training/...",
    ],
    "production": [
        "//apps/...",
        "//configs/...",
        "//control/...",
        "//data/...",
        "//evaluation/...",
        "//kernels/...",
        "//libs/...",
        "//models/...",
        "//preprocessing/...",
        "//protocols/...",
        "//sdk/...",
        "//services/...",
        "//serving/...",
        "//training/...",
    ],
    "source": [
        "//apps/...",
        "//configs/...",
        "//control/...",
        "//data/...",
        "//evaluation/...",
        "//kernels/...",
        "//libs/...",
        "//models/...",
        "//preprocessing/...",
        "//protocols/...",
        "//research/...",
        "//sdk/...",
        "//services/...",
        "//serving/...",
        "//training/...",
    ],
}

# Ordered most-specific first. The graph checker reports the first matching policy for a
# direct edge, so one dependency produces one actionable diagnostic.
BAZEL_FORBIDDEN_EDGES = [
    ["non_research", "boundary_research_experiments", "only research may consume experiments"],
    ["layer_serving", "boundary_training", "serving must consume published model contracts, not training internals"],
    ["boundary_apps", "boundary_services", "apps consume generated SDKs and contracts, not service implementations"],
    ["source", "boundary_infra", "source packages must not depend on deployment infrastructure"],
    ["production", "layer_research", "production packages must not depend on research"],
]

# Exact edge -> accepted ADR and rationale. The checker rejects malformed and stale entries;
# an exception cannot be a package-wide wildcard or an undocumented permanent bypass.
BAZEL_LAYER_EXCEPTIONS = {}

def declare_bazel_layer_package_groups():
    """Declare the centrally named groups in the calling package."""
    for name, packages in BAZEL_PACKAGE_GROUPS.items():
        native.package_group(
            name = name,
            packages = packages,
        )
