# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Repository-owned configuration for the Nix C/C++ toolchain."""

load("@rules_cc//cc:cc_toolchain_config_lib.bzl", "action_config", "feature", "flag_group", "flag_set", "tool", "tool_path")  # buildifier: disable=deprecated-function
load("@rules_cc//cc/common:cc_common.bzl", "cc_common")
load("@rules_cc//cc/toolchains:cc_toolchain_config_info.bzl", "CcToolchainConfigInfo")

_COMPILE_ACTIONS = [
    "assemble",
    "c-compile",
    "c++-compile",
    "c++-header-parsing",
    "c++-module-compile",
    "c++-module-codegen",
    "clif-match",
    "linkstamp-compile",
    "preprocess-assemble",
]

_LINK_ACTIONS = [
    "c++-link-dynamic-library",
    "c++-link-executable",
    "c++-link-nodeps-dynamic-library",
]

def _mindclade_cc_toolchain_config_impl(ctx):
    action_configs = [
        action_config(
            action_name = action_name,
            enabled = True,
            tools = [tool(path = ctx.attr.tools["cxx_linker"])],
        )
        for action_name in _LINK_ACTIONS
    ]
    features = []
    if ctx.attr.compile_flags:
        features.append(feature(
            name = "mindclade_default_compile_flags",
            enabled = True,
            flag_sets = [
                flag_set(
                    actions = _COMPILE_ACTIONS,
                    flag_groups = [flag_group(flags = ctx.attr.compile_flags)],
                ),
            ],
        ))
    if ctx.attr.link_flags:
        features.append(feature(
            name = "mindclade_default_link_flags",
            enabled = True,
            flag_sets = [
                flag_set(
                    actions = _LINK_ACTIONS,
                    flag_groups = [flag_group(flags = ctx.attr.link_flags)],
                ),
            ],
        ))
    features.extend([
        feature(name = "supports_dynamic_linker", enabled = True),
        feature(name = "supports_pic", enabled = True),
    ])

    return cc_common.create_cc_toolchain_config_info(
        ctx = ctx,
        action_configs = action_configs,
        abi_libc_version = "unknown",
        abi_version = "unknown",
        builtin_sysroot = ctx.attr.builtin_sysroot,
        compiler = "clang",
        cxx_builtin_include_directories = ctx.attr.builtin_include_directories,
        features = features,
        host_system_name = ctx.attr.system,
        target_cpu = ctx.attr.target_cpu,
        target_libc = "macos" if ctx.attr.system.endswith("darwin") else "glibc",
        target_system_name = ctx.attr.target_triple,
        tool_paths = [
            tool_path(name = name, path = path)
            for name, path in sorted(ctx.attr.tools.items())
            if name != "cxx_linker"
        ],
        toolchain_identifier = "mindclade-nix-{}".format(ctx.attr.system),
    )

mindclade_cc_toolchain_config = rule(
    implementation = _mindclade_cc_toolchain_config_impl,
    attrs = {
        "builtin_include_directories": attr.string_list(mandatory = True),
        "builtin_sysroot": attr.string(),
        "compile_flags": attr.string_list(),
        "link_flags": attr.string_list(),
        "system": attr.string(mandatory = True),
        "target_cpu": attr.string(mandatory = True),
        "target_triple": attr.string(mandatory = True),
        "tools": attr.string_dict(mandatory = True),
    },
    provides = [CcToolchainConfigInfo],
)
