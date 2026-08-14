{ pkgs, rustToolchain, versions, ... }:
pkgs.runCommand "mindclade-rust-version" { nativeBuildInputs = [ rustToolchain ]; } ''
  actual="$(${rustToolchain}/bin/rustc --version | awk '{print $2}')"
  test "$actual" = "${versions.rust}"
  touch "$out"
