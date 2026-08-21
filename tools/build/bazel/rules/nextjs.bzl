# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Mindclade Next.js build rules.

The upstream rules_js standalone macro is retained structurally, but Next 16 must
be invoked with its supported webpack backend: Turbopack rejects Bazel's sandboxed
package.json symlink before application compilation begins.
"""

load("@aspect_rules_js//js:defs.bzl", "js_run_binary")
load("@bazel_lib//lib:copy_directory.bzl", "copy_directory_bin_action")
load("@bazel_lib//lib:copy_file.bzl", "copy_file")

_NEXT_BUILD_CONFIG = "next.config.mjs"
_NEXT_BUILD_OUT = ".next"
_NEXT_STANDALONE_CONFIG = Label("@aspect_rules_js//contrib/nextjs:next.bazel.mjs")

def mindclade_nextjs_standalone_build(
        name,
        config,
        srcs,
        next_js_binary,
        data = [],
        use_execroot_entry_point = True,
        **kwargs):
    """Build a standalone Next.js application through the webpack backend."""
    tags = kwargs.pop("tags", [])
    testonly = kwargs.pop("testonly", False)
    visibility = kwargs.pop("visibility", [])
    config_basename = config.split(":")[-1].split("/")[-1]

    copy_file(
        name = "_%s.original_config_file" % name,
        src = config,
        out = "__original.%s" % config_basename,
        tags = tags + ["manual"],
        testonly = testonly,
        visibility = ["//visibility:private"],
    )

    env = dict(kwargs.pop("env", {}))
    env["NEXTJS_STANDALONE_CONFIG"] = "$(locations :_%s.original_config_file)" % name
    copy_file(
        name = "_%s.standalone_config_file" % name,
        src = _NEXT_STANDALONE_CONFIG,
        out = _NEXT_BUILD_CONFIG,
        tags = tags + ["manual"],
        testonly = testonly,
        visibility = ["//visibility:private"],
    )

    js_run_binary(
        name = "_%s.next_build" % name,
        tool = next_js_binary,
        args = ["build", "--webpack"],
        srcs = srcs + data + [
            ":_%s.standalone_config_file" % name,
            ":_%s.original_config_file" % name,
        ],
        out_dirs = [_NEXT_BUILD_OUT],
        chdir = native.package_name(),
        env = env,
        mnemonic = "NextJs",
        progress_message = "Compile Next.js standalone app %{label}",
        tags = tags + ["manual"],
        testonly = testonly,
        use_execroot_entry_point = use_execroot_entry_point,
        visibility = ["//visibility:private"],
        **kwargs
    )

    _copy_exec_to_bin(
        name = name,
        src = "_%s.next_build" % name,
        tags = tags,
        testonly = testonly,
        visibility = visibility,
    )

def _copy_exec_to_bin_impl(ctx):
    dst = ctx.actions.declare_directory(ctx.label.name)
    copy_directory_bin = ctx.toolchains["@bazel_lib//lib:copy_directory_toolchain_type"].copy_directory_info.bin
    copy_directory_bin_action(
        ctx,
        src = ctx.file.src,
        dst = dst,
        copy_directory_bin = copy_directory_bin,
    )
    return [
        DefaultInfo(
            files = depset([dst]),
            runfiles = ctx.runfiles([dst]),
        ),
    ]

_copy_exec_to_bin = rule(
    implementation = _copy_exec_to_bin_impl,
    attrs = {
        "src": attr.label(
            mandatory = True,
            allow_single_file = True,
            cfg = "exec",
        ),
    },
    toolchains = ["@bazel_lib//lib:copy_directory_toolchain_type"],
)
