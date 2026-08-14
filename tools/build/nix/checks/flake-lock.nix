{ pkgs, root, ... }:
pkgs.runCommand "mindclade-flake-lock" {} ''
  test -s ${root}/flake.lock || { echo "flake.lock is missing or empty" >&2; exit 1; }
  touch "$out"
