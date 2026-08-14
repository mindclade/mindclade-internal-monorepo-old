{ pkgs, versions, ... }:
let
  toolchain = pkgs.rust-bin.stable.${versions.rust}.default.override {
    extensions = [ "clippy" "rust-src" "rustfmt" ];
  };
in
{
  inherit toolchain;
  packages = [ toolchain ];
  version = versions.rust;
}
