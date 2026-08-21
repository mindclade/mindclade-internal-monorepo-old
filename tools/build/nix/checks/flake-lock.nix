# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

{ pkgs, root, ... }:
pkgs.runCommand "mindclade-flake-lock" { nativeBuildInputs = [ pkgs.python3 ]; } ''
  python3 - ${root}/flake.lock <<'PY'
  import json
  from pathlib import Path
  import re
  import sys
  from urllib.parse import urlsplit

  path = Path(sys.argv[1])
  if not path.is_file() or path.stat().st_size == 0:
      raise SystemExit("flake.lock is missing or empty")

  document = json.loads(path.read_text(encoding="utf-8"))
  if document.get("version") != 7:
      raise SystemExit(f"flake.lock schema must be 7, got {document.get('version')!r}")

  nodes = document.get("nodes")
  root_name = document.get("root")
  if not isinstance(nodes, dict) or root_name not in nodes:
      raise SystemExit("flake.lock has no valid root node")

  expected_inputs = {"nixpkgs": "nixpkgs", "rust-overlay": "rust-overlay"}
  if nodes[root_name].get("inputs") != expected_inputs:
      raise SystemExit("flake.lock root inputs differ from the reviewed flake contract")
  if nodes.get("rust-overlay", {}).get("inputs", {}).get("nixpkgs") != ["nixpkgs"]:
      raise SystemExit("rust-overlay must follow the root nixpkgs input")

  revision = re.compile(r"^[0-9a-f]{40}$")
  for name, node in sorted(nodes.items()):
      locked = node.get("locked")
      if locked is None:
          if name != root_name:
              raise SystemExit(f"flake input {name!r} is not locked")
          continue
      if locked.get("type") != "github":
          raise SystemExit(f"flake input {name!r} uses unapproved lock type {locked.get('type')!r}")
      if not revision.fullmatch(locked.get("rev", "")):
          raise SystemExit(f"flake input {name!r} is not pinned to a full commit revision")
      if not locked.get("narHash", "").startswith("sha256-"):
          raise SystemExit(f"flake input {name!r} has no SRI SHA-256 narHash")
      if not isinstance(locked.get("lastModified"), int):
          raise SystemExit(f"flake input {name!r} has no immutable lastModified evidence")

      for location in (locked, node.get("original", {})):
          url = location.get("url")
          if not isinstance(url, str):
              continue
          parsed = urlsplit(url)
          if parsed.username is not None or parsed.password is not None:
              raise SystemExit(f"flake input {name!r} embeds URL credentials")

  print("flake.lock: schema, inputs, follows, revisions, and hashes passed")
  PY
  touch "$out"
''
