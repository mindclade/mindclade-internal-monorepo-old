# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

{
  description = "Mindclade hermetic monorepo toolchains";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    rust-overlay.url = "github:oxalica/rust-overlay";
    rust-overlay.inputs.nixpkgs.follows = "nixpkgs";
  };

  outputs = { self, nixpkgs, rust-overlay }:
    let
      versions = import ./tools/build/nix/versions.nix;
      systems = [ "x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin" ];
      forAllSystems = fn: nixpkgs.lib.genAttrs systems (system:
        let
          pkgs = import nixpkgs { inherit system; overlays = [ rust-overlay.overlays.default ]; };
          rust = import ./tools/build/nix/toolchains/rust.nix { inherit pkgs versions; };
        in fn { inherit pkgs rust system; });
    in {
      devShells = forAllSystems ({ pkgs, rust, ... }: {
        default = pkgs.mkShell {
          packages = with pkgs; [ bazelisk buildifier buf go gotools nodejs_22 pnpm protobuf python312 ruff uv ] ++ rust.packages;
          shellHook = ''
            export MINDCLADE_REPO_ROOT="$PWD"
            export PYTHONNOUSERSITE=1
          '';
        };
        ci = pkgs.mkShell {
          packages = with pkgs; [ bazelisk buildifier buf cargo-deny go gotools nodejs_22 pnpm protobuf python312 ruff uv ] ++ rust.packages;
          shellHook = ''
            export MINDCLADE_REPO_ROOT="$PWD"
            export PYTHONNOUSERSITE=1
          '';
        };
      });

      checks = forAllSystems ({ pkgs, rust, ... }:
        import ./tools/build/nix/checks/default.nix {
          inherit pkgs versions;
          rustToolchain = rust.toolchain;
          root = self;
        });

      formatter = forAllSystems ({ pkgs, ... }: pkgs.nixfmt-rfc-style);
    };
}
