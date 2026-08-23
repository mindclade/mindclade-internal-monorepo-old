# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#
# The toolchain manifest: what the pinned closure ACTUALLY resolved to, as opposed to what
# versions.nix declares.
#
# ADR-0002 calls toolchain manifests release evidence and says CI rejects manifest drift. The
# two halves of that are here: this file renders the manifest, checks/toolchain-manifest.nix
# compares the rendered manifest against the committed copy, and `nix build .#toolchain-manifest`
# regenerates the committed copy when a change to the closure is intended.
#
# Why this is a different assertion from generated-files.nix. That check compares repository
# compat files against versions.nix — both sides are things a person wrote. This one compares
# nixpkgs' resolved package versions against a committed record, so it catches the case nobody
# wrote down at all: a flake.lock bump that silently moves go from 1.26.5 to 1.27.0. Declared
# pins are in here too, under `pins`, because a versions.nix edit that no compat file mirrors is
# also drift worth a diff.
#
# Versions, not store paths. A store path changes on any rebuild of any dependency — a stdenv
# bump moves every path in the closure without changing a single tool version — and a manifest
# that churns on rebuilds gets regenerated reflexively, which is the same as not having one.
# Versions plus the nixpkgs revision pin the closure well enough to be evidence, and the
# revision is what makes the paths recoverable when they are actually needed.
#
# The manifest is system-independent because these packages carry one version per nixpkgs
# revision across all four supported systems. If a package ever resolves differently per system,
# this attrset grows a per-system section rather than the check growing an exception.

{
  pkgs,
  root,
  versions,
  ...
}:

let
  # The lock is read through the flake source rather than passed in from flake.nix, so the
  # manifest can be rendered by anything that can import this file.
  lock = builtins.fromJSON (builtins.readFile "${root}/flake.lock");

  nixpkgsRev = lock.nodes.nixpkgs.locked.rev;

  # The default devShell's package set, by the attribute name flake.nix uses. Keep the two in
  # step: a tool added to the shell and not to this list is a tool whose version can move
  # without evidence.
  tools = {
    bazel = pkgs.bazel_9.version;
    buf = pkgs.buf.version;
    buildifier = pkgs.buildifier.version;
    go = pkgs.go.version;
    nodejs = pkgs.nodejs_22.version;
    pnpm = pkgs.pnpm.version;
    protobuf = pkgs.protobuf.version;
    python = pkgs.python314.version;
    ruff = pkgs.ruff.version;
    uv = pkgs.uv.version;

    # Not from pkgs: the Rust toolchain comes from the rust-overlay at the exact version
    # versions.rust names, and checks/rust-version.nix is what proves the binary agrees.
    rust = versions.rust;
  };

  # What `.#ci` adds on top of the default shell. These are the tools that decide whether a pull
  # request merges, so they are exactly the ones whose versions have to be evidence rather than
  # whatever the lock happened to resolve that week:
  #
  #   * terraform's output is version-sensitive, and infrastructure-live consumes infra/terraform
  #     as a module source — a silent bump changes plan output in a DIFFERENT repository;
  #   * actionlint and shellcheck disagree across versions about which findings exist at all
  #     (SC2153, SC2015), so "passes locally, fails in CI" is a version question;
  #   * cargo-deny and trivy security verdicts move with their databases and their own
  #     release lines.
  ciTools = {
    actionlint = pkgs.actionlint.version;
    cargo-deny = pkgs.cargo-deny.version;
    conftest = pkgs.conftest.version;
    git = pkgs.git.version;
    helm = pkgs.kubernetes-helm.version;
    kubeconform = pkgs.kubeconform.version;
    kustomize = pkgs.kustomize.version;
    promtool = pkgs.prometheus.version;
    pyyaml = pkgs.python314Packages.pyyaml.version;
    shellcheck = pkgs.shellcheck.version;
    terraform = pkgs.terraform.version;
    terraform-docs = pkgs.terraform-docs.version;
    trivy = pkgs.trivy.version;
    yamllint = pkgs.yamllint.version;
    yq = pkgs.yq-go.version;
  };

  attrs = {
    schema = 2;
    nixpkgs = nixpkgsRev;
    pins = versions;
    cxx = {
      compiler = "clang";
      darwinDeploymentTarget = versions.darwinDeploymentTarget;
      darwinSystemLibraries = [
        "iconv"
        "resolv"
        "sbuf"
        "util"
      ];
    };
    inherit tools;
    ciTools = ciTools;
  };

in
{
  inherit attrs;

  # Rendered through python rather than emitted with builtins.toJSON directly: toJSON produces
  # one unsorted line, and this file is committed and reviewed, so it has to diff cleanly.
  file =
    pkgs.runCommand "toolchain-manifest.json"
      {
        nativeBuildInputs = [ pkgs.python3 ];
        raw = builtins.toJSON attrs;
        passAsFile = [ "raw" ];
      }
      ''
        python3 - <<'PY'
        import json
        import os

        with open(os.environ["rawPath"]) as handle:
            manifest = json.load(handle)

        with open(os.environ["out"], "w") as handle:
            json.dump(manifest, handle, indent=2, sort_keys=True)
            handle.write("\n")
        PY
      '';
}
