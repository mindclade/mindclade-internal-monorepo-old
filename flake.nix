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
          # allowUnfree, narrowly. Terraform is BUSL-licensed since 1.6, so `nix develop .#ci`
          # fails to EVALUATE without this — not at the terraform step, but before the shell
          # exists, with a licence error that names no package usefully. The CUDA entries are
          # NVIDIA's redistributable licence, unfree in the same nixpkgs sense; ADR-0002 makes
          # Nix the toolchain authority and there is no free path to a CUDA compiler.
          #
          # A predicate rather than a blanket `allowUnfree = true` so the next unfree dependency
          # has to be added deliberately rather than inherited. bootstrap's flake takes the
          # blanket form; this is the same decision made narrower.
          pkgs = import nixpkgs {
            inherit system;
            overlays = [ rust-overlay.overlays.default ];
            config.allowUnfreePredicate = pkg:
              let
                name = nixpkgs.lib.getName pkg;
              in
              # Named exactly, because it is one package under one licence.
              builtins.elem name [ "terraform" ]

              # The CUDA closure, by prefix rather than by name. The enumerated form listed the
              # packages `gpu`'s `packages` line asks for and nothing they pull in, so evaluation
              # died on cuda_nvrtc — a transitive dependency of cuda_nvcc that no list written by
              # reading the shell definition would contain. Extending the list one name per failed
              # evaluation is not a smaller decision than this, it is the same decision made once
              # per nixpkgs bump.
              #
              # Still deliberate in the sense the enumerated form was reaching for: these prefixes
              # are the NVIDIA redistributable families and nothing else, so an unfree package
              # from any other vendor fails evaluation exactly as before, and darwin never reaches
              # this branch because config.cudaSupport gates the shell that needs it.
              || builtins.any (prefix: nixpkgs.lib.hasPrefix prefix name) [
                "cuda"
                "cudnn"
                "cutensor"
                "libcu"
                "libnpp"
                "libnv"
                "nccl"
                "nvidia"
              ];
            config.cudaSupport = system == "x86_64-linux";
          };
          rust = import ./tools/build/nix/toolchains/rust.nix { inherit pkgs versions; };
        in fn { inherit pkgs rust system; });
    in {
      devShells = forAllSystems ({ pkgs, rust, system, ... }: {
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
      } // nixpkgs.lib.optionalAttrs (system == "x86_64-linux") {
        # ---------------------------------------------------------------------------------
        # GPU shell
        # ---------------------------------------------------------------------------------
        # x86_64-linux ONLY, and the guard is load-bearing rather than tidiness: cudaPackages
        # does not evaluate on darwin, so an unguarded attribute makes `nix flake check` and
        # even `nix develop` fail on a laptop with an error about a package nobody asked for.
        # The GPU nodes are amd64; there is no aarch64 CUDA target in this estate.
        #
        # This is NOT what runs torch. The torch wheels resolved through uv.lock bundle their
        # own CUDA runtime — the 43 nvidia-* entries in that lock, all marked
        # `sys_platform == 'linux'`. Installing a second runtime beside them is how you get two
        # libcudart on one LD_LIBRARY_PATH and a crash that names neither.
        #
        # What this shell is for is the part the wheels do not carry: COMPILING. TileLang
        # kernels (ADR-0008) need nvcc, the CUDA headers, and cuDNN/NCCL to link against, and
        # `kernels/` has 41 BUILD.bazel files that will need them the moment they stop being
        # filegroups. Keeping it a separate shell rather than adding it to `default` means the
        # 3 GB closure is paid by the people building kernels and by nobody else.
        gpu = pkgs.mkShell {
          packages = (with pkgs; [ bazelisk buildifier go gotools protobuf python312 ruff uv ])
            ++ (with pkgs.cudaPackages; [ cuda_nvcc cuda_cudart cuda_cccl cudnn nccl libcublas ])
            ++ rust.packages;
          shellHook = ''
            export MINDCLADE_REPO_ROOT="$PWD"
            export PYTHONNOUSERSITE=1

            # nvcc finds the headers and the stub libraries through CUDA_PATH. Set here rather
            # than left to the caller because a missing CUDA_PATH produces "cannot find
            # -lcudart" at link time, which reads like a broken build file.
            export CUDA_PATH="${pkgs.cudaPackages.cuda_nvcc}"
            export CUDA_HOME="$CUDA_PATH"
          '';
        };
      });

      checks = forAllSystems ({ pkgs, rust, ... }:
        import ./tools/build/nix/checks/default.nix {
          inherit pkgs versions;
          rustToolchain = rust.toolchain;
          root = self;
        });

      # The regeneration side of checks/toolchain-manifest.nix. That check compares the
      # committed manifest against what the flake resolves; this is how you produce a new
      # committed manifest when the closure moved on purpose:
      #
      #   nix build .#toolchain-manifest
      #   install -m 0644 result tools/build/nix/toolchain-manifest.json
      #
      # Exposed as a package rather than hidden inside the check so the evidence the ADR calls
      # for is buildable on its own, without running the whole check set.
      packages = forAllSystems ({ pkgs, ... }:
        let
          manifest = import ./tools/build/nix/manifest.nix {
            inherit pkgs versions;
            root = self;
          };
        in {
          toolchain-manifest = manifest.file;
        });

      formatter = forAllSystems ({ pkgs, ... }: pkgs.nixfmt-rfc-style);
    };
}
