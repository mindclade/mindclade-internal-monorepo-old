"""Reusable, network-isolated Terraform module test rule."""

load("@rules_shell//shell:sh_test.bzl", _sh_test = "sh_test")


def _unique(values):
    result = []
    for value in values:
        if value not in result:
            result.append(value)
    return result


def terraform_test(
        name,
        module_files = None,
        module_marker = "variables.tf",
        srcs = None,
        data = None,
        env = None,
        tags = None,
        visibility = None,
        **kwargs):
    """Runs terraform init and terraform test from an offline provider mirror.

    srcs is accepted only to make migration from a raw sh_test atomic; the
    reusable runner always owns the executable. Existing data and env values are
    layered underneath the controlled tool, provider, and module markers.
    """
    _ = srcs
    controlled_env = dict(env or {})
    controlled_env.update({
        "MINDCLADE_TERRAFORM_MODULE_MARKER": "$(rlocationpath %s)" % module_marker,
        "MINDCLADE_TERRAFORM_PROVIDER_MIRROR_MARKER": "$(rlocationpath @mindclade_terraform_google_provider//:provider_root)",
        "MINDCLADE_TERRAFORM_TOOL_MARKER": "$(rlocationpath @mindclade_infra_validation_tools//:bin/.tool-root)",
    })
    rule_args = dict(
        name = name,
        srcs = ["//tools/build/bazel:terraform_test_runner.sh"],
        data = _unique((data or []) + (module_files or []) + [
            module_marker,
            "@mindclade_infra_validation_tools//:bin/.tool-root",
            "@mindclade_infra_validation_tools//:tools",
            "@mindclade_terraform_google_provider//:provider",
            "@mindclade_terraform_google_provider//:provider_root",
        ]),
        env = controlled_env,
        tags = _unique((tags or []) + ["terraform", "offline"]),
    )
    if visibility != None:
        rule_args["visibility"] = visibility
    rule_args.update(kwargs)
    _sh_test(**rule_args)
