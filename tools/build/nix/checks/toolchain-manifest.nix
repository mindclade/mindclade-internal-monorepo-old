# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#
# ADR-0002 enforcement: "CI rejects ... toolchain-manifest drift", against consequence
# "Toolchain manifests and execution-image digests are release evidence."
#
# The committed manifest at tools/build/nix/toolchain-manifest.json is the record of which
# toolchain the repository builds with. This check re-renders it from the flake's own resolution
# and fails when the two differ, which happens in exactly the cases the ADR cares about:
#
#   * flake.lock moved, so nixpkgs resolves different tool versions than the record claims;
#   * versions.nix moved, so the declared pins no longer match the record;
#   * the manifest was hand-edited to say something the closure does not.
#
# The failure prints a field-by-field diff and the regeneration command, because a manifest
# check whose remedy is unclear gets bypassed rather than fixed.

{ pkgs, root, versions, ... }:

let
  manifest = import ../manifest.nix { inherit pkgs root versions; };
in
pkgs.runCommand "mindclade-toolchain-manifest"
  {
    nativeBuildInputs = [ pkgs.python3 ];
  }
  ''
    python3 - <<'PY'
    import json
    import sys
    from pathlib import Path

    committed_path = Path("${root}/tools/build/nix/toolchain-manifest.json")
    rendered_path = Path("${manifest.file}")

    if not committed_path.is_file():
        print(
            "toolchain-manifest: tools/build/nix/toolchain-manifest.json does not exist.\n"
            "Generate it with:\n"
            "  nix build .#toolchain-manifest\n"
            "  install -m 0644 result tools/build/nix/toolchain-manifest.json",
            file=sys.stderr,
        )
        raise SystemExit(1)

    try:
        committed = json.loads(committed_path.read_text())
    except json.JSONDecodeError as exc:
        print(f"toolchain-manifest: committed manifest is not valid JSON: {exc}", file=sys.stderr)
        raise SystemExit(1)

    rendered = json.loads(rendered_path.read_text())


    def flatten(value, prefix=""):
        """Field paths to scalars, so the diff names `tools.go` rather than `tools`."""
        if isinstance(value, dict):
            flat = {}
            for key, inner in value.items():
                flat.update(flatten(inner, f"{prefix}{key}."))
            return flat
        return {prefix.rstrip("."): value}


    committed_flat = flatten(committed)
    rendered_flat = flatten(rendered)

    drift = []
    for field in sorted(set(committed_flat) | set(rendered_flat)):
        was = committed_flat.get(field, "<absent>")
        now = rendered_flat.get(field, "<absent>")
        if was != now:
            drift.append(f"  {field}\n      committed: {was}\n      resolved:  {now}")

    if drift:
        print("toolchain-manifest: the resolved toolchain no longer matches the committed evidence.", file=sys.stderr)
        print("", file=sys.stderr)
        print("\n".join(drift), file=sys.stderr)
        print("", file=sys.stderr)
        print(
            "If the change is intended, regenerate the manifest in the same commit:\n"
            "  nix build .#toolchain-manifest\n"
            "  install -m 0644 result tools/build/nix/toolchain-manifest.json",
            file=sys.stderr,
        )
        raise SystemExit(1)
    PY

    touch "$out"
  ''
