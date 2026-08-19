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
          # allowUnfree, for terraform and nothing else. Terraform is BUSL-licensed since 1.6,
          # so `nix develop .#ci` fails to EVALUATE without this — not at the terraform step,
          # but before the shell exists, with a licence error that names no package usefully.
          # A predicate rather than a blanket `allowUnfree = true` so that the next unfree
          # dependency has to be added deliberately rather than inherited. bootstrap's flake
          # takes the blanket form; this is the same decision made narrower.
          pkgs = import nixpkgs {
            inherit system;
            overlays = [ rust-overlay.overlays.default ];
            config.allowUnfreePredicate = pkg:
              builtins.elem (nixpkgs.lib.getName pkg) [ "terraform" ];
          };
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
          # actionlint/shellcheck/yamllint feed the `lint` lane, terraform the `terraform`
          # lane. Both sets of configs (.yamllint.yaml, .github/actionlint.yaml) existed in
          # the estate's other repositories with nothing running them; here they did not
          # exist at all, and neither did a Terraform gate over infra/ — which
          # infrastructure-live consumes as a module source, so a broken module there breaks
          # a different repository's plan.
          packages = with pkgs; [ actionlint bazelisk buildifier buf cargo-deny go gotools nodejs_22 pnpm protobuf python312 ruff shellcheck terraform uv yamllint ] ++ rust.packages;
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
