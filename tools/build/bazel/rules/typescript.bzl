# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Shared, hermetic TypeScript test wrappers."""

load("@npm//:tsx/package_json.bzl", _tsx_bin = "bin")

def typescript_test(name, entry_point, srcs, data = [], **kwargs):
    """Run Node's test API through the lockfile-pinned tsx executable."""
    _tsx_bin.tsx_test(
        name = name,
        args = [entry_point],
        chdir = native.package_name(),
        data = srcs + data,
        **kwargs
    )
