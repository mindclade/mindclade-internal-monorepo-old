# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

{
  pkgs,
  versions,
  system,
  ...
}:

let
  cc = pkgs.llvmPackages.stdenv.cc;
  isDarwin = pkgs.stdenv.hostPlatform.isDarwin;
  targetCpu =
    {
      aarch64-darwin = "darwin_arm64";
      aarch64-linux = "aarch64";
      x86_64-linux = "k8";
    }
    .${system} or (throw "unsupported Mindclade C/C++ toolchain system: ${system}");
  constraintOs = if isDarwin then "osx" else "linux";
  constraintCpu = if pkgs.stdenv.hostPlatform.isAarch64 then "aarch64" else "x86_64";
  # Apple ships these libraries with the platform, but nixpkgs deliberately splits them out
  # of the SDK. Bazel actions run with a strict environment, so the NIX_LDFLAGS that mkShell
  # normally supplies are unavailable. Carry the pinned library roots in the registered
  # toolchain instead of falling back to /usr/lib on the host.
  darwinLinkLibraries =
    if isDarwin then
      [
        pkgs.libiconv
        pkgs.darwin.libresolv
        pkgs.darwin.libsbuf
        pkgs.darwin.libutil
      ]
    else
      [ ];
  darwinLinkLibraryDirectories = map (library: "${pkgs.lib.getLib library}/lib") darwinLinkLibraries;
in
pkgs.runCommand "mindclade-cc-toolchain-bundle-${system}"
  {
    nativeBuildInputs = [ pkgs.python3 ];
    preferLocalBuild = true;
    allowSubstitutes = true;
  }
  ''
    set -euo pipefail

    mkdir -p "$out/bin"

    compiler=${cc}/bin/clang
    compiler_cxx=${cc}/bin/clang++
    if [[ ! -x "$compiler" || ! -x "$compiler_cxx" ]]; then
      echo "cc-toolchain-bundle: LLVM stdenv does not provide clang and clang++" >&2
      exit 1
    fi

    for tool in clang clang++ ar ld nm objcopy objdump strip; do
      candidate=${cc}/bin/$tool
      if [[ ! -x "$candidate" ]]; then
        echo "cc-toolchain-bundle: required tool is missing: $candidate" >&2
        exit 1
      fi
      ln -s "$candidate" "$out/bin/$tool"
    done

    resource_dir="$($compiler -print-resource-dir)"
    if [[ ! -d "$resource_dir/include" ]]; then
      echo "cc-toolchain-bundle: compiler resource headers are missing: $resource_dir/include" >&2
      exit 1
    fi

    sdk_root="${
      if isDarwin then "${pkgs.apple-sdk}/Platforms/MacOSX.platform/Developer/SDKs/MacOSX.sdk" else ""
    }"
    if [[ ${if isDarwin then "1" else "0"} == 1 && ! -d "$sdk_root" ]]; then
      echo "cc-toolchain-bundle: Apple SDK is missing: $sdk_root" >&2
      exit 1
    fi

    target_triple="$($compiler -dumpmachine)"
    "$compiler_cxx" -E -x c++ -v - </dev/null 2>includes.txt >/dev/null

    export bundle_system=${system}
    export bundle_target_cpu=${targetCpu}
    export bundle_constraint_os=${constraintOs}
    export bundle_constraint_cpu=${constraintCpu}
    export bundle_target_triple="$target_triple"
    export bundle_resource_dir="$resource_dir"
    export bundle_sdk_root="$sdk_root"
    export bundle_deployment_target=${versions.darwinDeploymentTarget}
    export bundle_link_library_directories='${builtins.toJSON darwinLinkLibraryDirectories}'
    export bundle_out="$out"

    python3 - <<'PY'
    import json
    import os
    from pathlib import Path

    include_dirs = []
    recording = False
    for raw_line in Path("includes.txt").read_text().splitlines():
        line = raw_line.strip()
        if line == "#include <...> search starts here:":
            recording = True
            continue
        if recording and line == "End of search list.":
            break
        if recording:
            include = line.removesuffix(" (framework directory)")
            if Path(include).is_dir() and include not in include_dirs:
                include_dirs.append(include)

    resource_include = str(Path(os.environ["bundle_resource_dir"]) / "include")
    if resource_include not in include_dirs:
        include_dirs.insert(0, resource_include)

    sdk_root = os.environ["bundle_sdk_root"]
    link_library_directories = json.loads(os.environ["bundle_link_library_directories"])
    compile_flags = []
    link_flags = []
    if sdk_root:
        deployment_target = os.environ["bundle_deployment_target"]
        compile_flags = ["-isysroot", sdk_root, f"-mmacosx-version-min={deployment_target}"]
        link_flags = ["-isysroot", sdk_root, f"-mmacosx-version-min={deployment_target}"]
        link_flags.extend(f"-L{directory}" for directory in link_library_directories)

    manifest = {
        "schema": 1,
        "system": os.environ["bundle_system"],
        "target_cpu": os.environ["bundle_target_cpu"],
        "target_triple": os.environ["bundle_target_triple"],
        "constraints": {
            "cpu": os.environ["bundle_constraint_cpu"],
            "os": os.environ["bundle_constraint_os"],
        },
        "compiler": "clang",
        "compiler_version": os.popen(
            f"{os.environ['bundle_out']}/bin/clang --version"
        ).readline().strip(),
        "resource_dir": os.environ["bundle_resource_dir"],
        "builtin_include_directories": include_dirs,
        "sdk_root": sdk_root,
        "darwin_deployment_target": (
            os.environ["bundle_deployment_target"] if sdk_root else None
        ),
        "compile_flags": compile_flags,
        "link_flags": link_flags,
        "link_library_directories": link_library_directories,
        "tools": {
            "ar": "bin/ar",
            "cpp": "bin/clang++",
            "gcc": "bin/clang",
            "cxx_linker": "bin/clang++",
            "ld": "bin/ld",
            "nm": "bin/nm",
            "objcopy": "bin/objcopy",
            "objdump": "bin/objdump",
            "strip": "bin/strip",
        },
    }

    with Path(os.environ["bundle_out"], "manifest.json").open("w") as handle:
        json.dump(manifest, handle, indent=2, sort_keys=True)
        handle.write("\n")
    PY
  ''
