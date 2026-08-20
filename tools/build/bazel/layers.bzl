# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#
"""Fail-closed repository dependency-layer policy.

Keep the assignments literal. tools/analysis/check_bazel_layers.py reads this file with the
Python AST so BUILD visibility declarations and CI graph governance share one source of truth.
"""

# Every internal Bazel package must match exactly one layer. Support layers are deliberately
# narrow: production code may use root metadata and build/test/release helpers without gaining
# access to deployment, CI, architecture, or arbitrary developer tooling.
BAZEL_LAYERS = {
    "apps": ["//apps/..."],
    "build_support": ["//tools/build/..."],
    "foundation": [
        "//configs/...",
        "//libs/...",
        "//protocols/...",
        "//sdk/...",
    ],
    "offline": [
        "//data/...",
        "//evaluation/...",
        "//kernels/...",
        "//models/...",
        "//preprocessing/...",
    ],
    "platform": [
        "//architecture/...",
        "//ci/...",
        "//docs/...",
        "//examples/...",
        "//infra/...",
        "//qualification/...",
        "//security/...",
        "//tools",
        "//tools/analysis/...",
        "//tools/codegen/...",
        "//tools/dev/...",
        "//tools/license/...",
        "//tools/qualification/...",
    ],
    "release_support": ["//tools/release/..."],
    "research": ["//research/..."],
    "root_support": ["//"],
    "runtime": [
        "//control/...",
        "//serving/...",
    ],
    "services": ["//services/..."],
    "test_support": ["//tests/..."],
    "training": ["//training/..."],
}

# Explicit source -> allowed destination matrix. New dependency directions are denied until
# reviewed here; there is no implicit "not forbidden means allowed" path.
BAZEL_LAYER_ALLOW_MATRIX = {
    "apps": [
        "apps",
        "build_support",
        "foundation",
        "release_support",
        "root_support",
        "test_support",
    ],
    "build_support": [
        "apps",
        "build_support",
        "foundation",
        "offline",
        "platform",
        "release_support",
        "research",
        "root_support",
        "runtime",
        "services",
        "test_support",
        "training",
    ],
    "foundation": [
        "build_support",
        "foundation",
        "root_support",
        "test_support",
    ],
    "offline": [
        "build_support",
        "foundation",
        "offline",
        "release_support",
        "root_support",
        "test_support",
    ],
    "platform": [
        "apps",
        "build_support",
        "foundation",
        "offline",
        "platform",
        "release_support",
        "research",
        "root_support",
        "runtime",
        "services",
        "test_support",
        "training",
    ],
    "release_support": [
        "apps",
        "build_support",
        "foundation",
        "offline",
        "platform",
        "release_support",
        "research",
        "root_support",
        "runtime",
        "services",
        "test_support",
        "training",
    ],
    "research": [
        "apps",
        "build_support",
        "foundation",
        "offline",
        "platform",
        "release_support",
        "research",
        "root_support",
        "runtime",
        "services",
        "test_support",
        "training",
    ],
    "root_support": [
        "apps",
        "build_support",
        "foundation",
        "offline",
        "platform",
        "release_support",
        "research",
        "root_support",
        "runtime",
        "services",
        "test_support",
        "training",
    ],
    "runtime": [
        "build_support",
        "foundation",
        "offline",
        "release_support",
        "root_support",
        "runtime",
        "test_support",
    ],
    "services": [
        "build_support",
        "foundation",
        "offline",
        "release_support",
        "root_support",
        "runtime",
        "services",
        "test_support",
    ],
    "test_support": [
        "apps",
        "build_support",
        "foundation",
        "offline",
        "platform",
        "release_support",
        "research",
        "root_support",
        "runtime",
        "services",
        "test_support",
        "training",
    ],
    "training": [
        "build_support",
        "foundation",
        "offline",
        "release_support",
        "root_support",
        "test_support",
        "training",
    ],
}

# Exact live edge -> temporary exception metadata. Exceptions are capped at 90 days and the
# checker validates the owner, accepted ADR, rationale, expiry, and edge liveness.
BAZEL_LAYER_EXCEPTIONS = {}

def declare_bazel_layer_package_groups():
    """Declare visibility groups mirroring the classifier."""
    for name, packages in BAZEL_LAYERS.items():
        native.package_group(
            name = "layer_{}".format(name),
            packages = packages,
        )
