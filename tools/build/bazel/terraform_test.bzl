# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#
"""Reusable, network-isolated Terraform module tests."""

load("@rules_shell//shell:sh_test.bzl", _sh_test = "sh_test")

def _unique(values):
    result = []
    for value in values:
        if value not in result:
            result.append(value)
    return result

def terraform_test(
        name,
        module_files,
        module_marker = "variables.tf",
        data = None,
        env = None,
        tags = None,
        visibility = None,
        **kwargs):
    """Runs terraform init and terraform test against the pinned provider mirror."""
    controlled_env = dict(env or {})
    controlled_env.update({
        "MINDCLADE_TERRAFORM_MODULE_MARKER_RLOCATION": "$(rlocationpath %s)" % module_marker,
        "MINDCLADE_TERRAFORM_PROVIDER_MIRROR_MARKER_RLOCATION": "$(rlocationpath @mindclade_terraform_google_provider//:provider_root)",
        "MINDCLADE_TERRAFORM_RLOCATION": "$(rlocationpath @mindclade_terraform_google_provider//:terraform)",
    })
    rule_args = dict(
        name = name,
        srcs = ["//tools/build/bazel:terraform_test_runner.sh"],
        data = _unique((data or []) + module_files + [
            module_marker,
            "@mindclade_terraform_google_provider//:provider",
            "@mindclade_terraform_google_provider//:provider_root",
            "@mindclade_terraform_google_provider//:terraform",
        ]),
        env = controlled_env,
        tags = _unique((tags or []) + ["offline", "terraform"]),
    )
    if visibility != None:
        rule_args["visibility"] = visibility
    rule_args.update(kwargs)
    _sh_test(**rule_args)
