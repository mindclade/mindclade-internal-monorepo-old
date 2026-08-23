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

  outputs =
    {
      self,
      nixpkgs,
      rust-overlay,
    }:
    let
      versions = import ./tools/build/nix/versions.nix;
      # nixos-unstable can lag a Go security point release by a few days. Keep the
      # compiler closure fail-closed during that window instead of allowing the
      # dev shell to retain a standard library with reachable vulnerabilities.
      # The source hash is for the upstream go1.26.6 release tarball.
      goSecurityOverlay = final: prev: {
        go = prev.go.overrideAttrs (_: {
          version = "1.26.6";
          src = final.fetchurl {
            url = "https://go.dev/dl/go1.26.6.src.tar.gz";
            hash = "sha256-oHIcVMaIkBRI13rZs+x+p8R0cwdV/4kTgukuy5P/LLE=";
          };
        });
      };
      # x86_64-darwin is NOT here, and its absence is the fix rather than an oversight: nixpkgs
      # 26.11 — the revision flake.lock pins — dropped the platform outright, so every attribute
      # generated for it failed to evaluate with "Nixpkgs 26.11 has dropped support for
      # x86_64-darwin". That made `nix flake check --all-systems` and `nix flake show
      # --all-systems` fail on a platform nobody in this estate builds on, while the per-system
      # commands people actually run kept passing, so nothing surfaced it.
      #
      # Restoring Intel Mac support means pinning the nixpkgs input to the 26.05 darwin branch,
      # which is a toolchain decision for ADR-0002 and not a line in the systems list.
      systems = [
        "x86_64-linux"
        "aarch64-linux"
        "aarch64-darwin"
      ];
      forAllSystems =
        fn:
        nixpkgs.lib.genAttrs systems (
          system:
          let
            # allowUnfree, narrowly. Terraform is BUSL-licensed since 1.6, so `nix develop .#ci`
            # fails to EVALUATE without this — not at the terraform step, but before the shell
            # exists, with a licence error that names no package usefully. The CUDA entries are
            # NVIDIA's redistributable licence, unfree in the same nixpkgs sense; ADR-0002 makes
            # Nix the toolchain authority and there is no free path to a CUDA compiler.
            #
            # A predicate rather than a blanket `allowUnfree = true` so the next unfree dependency
            # has to be added deliberately rather than inherited. The estate control-plane
            # flakes apply the same package-name predicate to Terraform.
            pkgs = import nixpkgs {
              inherit system;
              overlays = [
                rust-overlay.overlays.default
                goSecurityOverlay
              ];
              config.allowUnfreePredicate =
                pkg:
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
                  # Formerly cuda_cccl. Named here because the rename dropped the prefix that the
                  # rest of this list matches on.
                  "cccl"
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
            ccToolchain = import ./tools/build/nix/toolchains/cc.nix {
              inherit pkgs versions system;
            };
            # nixpkgs builds Prometheus from source. On an uncached GitHub runner that made the
            # Kubernetes lane spend more than twenty minutes compiling a Go server just to get
            # its small `promtool` companion binary. Use Prometheus' upstream release payload
            # with fixed hashes and install only the executable the validators invoke.
            # The version assertion makes a nixpkgs bump request an explicit hash review rather
            # than silently pairing a new manifest version with stale release bytes.
            promtoolVersion = "3.13.2";
            promtoolArtifact =
              {
                x86_64-linux = {
                  platform = "linux-amd64";
                  hash = "sha256-DoxNRhAb0CXqgmXjd9LKq8V/SI/Bvhw2fzfbaepBvm8=";
                };
                aarch64-linux = {
                  platform = "linux-arm64";
                  hash = "sha256-fOyxem9B1ZgU4aBYGh+B95BRrVlz0ezzniOp90fWVyo=";
                };
                aarch64-darwin = {
                  platform = "darwin-arm64";
                  hash = "sha256-9oyk8dvt1jZrv92KxdLAt7ofJzR0rMjTjrMyAvvux6Q=";
                };
              }
              .${system};
            promtoolBinary =
              assert pkgs.prometheus.version == promtoolVersion;
              pkgs.stdenvNoCC.mkDerivation {
                pname = "promtool-bin";
                version = promtoolVersion;
                src = pkgs.fetchurl {
                  url = "https://github.com/prometheus/prometheus/releases/download/v${promtoolVersion}/prometheus-${promtoolVersion}.${promtoolArtifact.platform}.tar.gz";
                  inherit (promtoolArtifact) hash;
                };
                sourceRoot = "prometheus-${promtoolVersion}.${promtoolArtifact.platform}";
                dontConfigure = true;
                dontBuild = true;
                installPhase = ''
                  runHook preInstall
                  install -Dm755 promtool "$out/bin/promtool"
                  runHook postInstall
                '';
              };
          in
          fn {
            inherit
              ccToolchain
              pkgs
              promtoolBinary
              rust
              system
              ;
          }
        );
      flakeLock = builtins.fromJSON (builtins.readFile ./flake.lock);
      flakeLockSha256 = builtins.hashFile "sha256" ./flake.lock;
      sourceRevision =
        if self ? rev then
          self.rev
        else if self ? dirtyRev then
          self.dirtyRev
        else
          "unknown";
      workstationSystem = nixpkgs.lib.nixosSystem {
        modules = [
          {
            nixpkgs.hostPlatform = "x86_64-linux";
            nixpkgs.overlays = [ goSecurityOverlay ];
          }
          (import ./infra/nixos/workstation {
            inherit flakeLockSha256 sourceRevision;
            nixpkgsRevision = flakeLock.nodes.nixpkgs.locked.rev;
          })
        ];
      };
    in
    {
      nixosConfigurations.mindclade-workstation = workstationSystem;

      devShells = forAllSystems (
        {
          pkgs,
          promtoolBinary,
          rust,
          ccToolchain,
          system,
          ...
        }:
        let
          # rules_python asks xcode-select for a CommandLineTools-shaped SDK path while
          # materializing wheel repositories. Nix's xcbuild implementation instead reports
          # the bare SDK derivation, which rules_python mistakes for full Xcode and follows
          # with an unavailable xcodebuild call. Keep this compatibility adapter in the Nix
          # closure (not the Bazel launcher) and point it at the pinned Nix SDK.
          xcodeSelectCompat =
            if pkgs.stdenv.hostPlatform.isDarwin then
              pkgs.runCommand "mindclade-nix-xcode-select" { } ''
                mkdir -p "$out/bin" "$out/CommandLineTools/SDKs"
                ln -s ${pkgs.apple-sdk}/Platforms/MacOSX.platform/Developer/SDKs/MacOSX.sdk \
                  "$out/CommandLineTools/SDKs/MacOSX.sdk"
                printf '%s\n' \
                  '#!/usr/bin/env bash' \
                  'set -euo pipefail' \
                  'if [[ "''${1:-}" != "--print-path" && "''${1:-}" != "-p" ]]; then' \
                  '  echo "xcode-select: the Nix compatibility adapter supports --print-path only" >&2' \
                  '  exit 2' \
                  'fi' \
                  'echo "${placeholder "out"}/CommandLineTools"' \
                  > "$out/bin/xcode-select"
                chmod +x "$out/bin/xcode-select"
              ''
            else
              null;
          baseShellHook = ''
            # Reusable qualification invokes the shell with `--ignore-environment`. Bazel's
            # output/cache resolver requires HOME even when the binary itself is repository
            # pinned, so provide an isolated disposable home instead of inheriting developer
            # state or failing before the wrapper can verify .bazelversion.
            if [ -z "''${HOME:-}" ]; then
              export HOME="''${TMPDIR:?}/mindclade-nix-home"
              mkdir -p "$HOME"
            fi
            export MINDCLADE_REPO_ROOT="$PWD"
            export PYTHONNOUSERSITE=1
          '';
          standardShellHook =
            baseShellHook
            + ''
              export MINDCLADE_CC_TOOLCHAIN_ROOT="${ccToolchain}"
            ''
            + nixpkgs.lib.optionalString pkgs.stdenv.hostPlatform.isDarwin ''
              export PATH="${xcodeSelectCompat}/bin:$PATH"
            '';
          trustedGitShellHook = standardShellHook + ''
            export MINDCLADE_GIT="${pkgs.git}/bin/git"
          '';
          atticClient =
            assert pkgs.attic-client.src.rev == "7a19204df10d606c5070e6bb72615c3461900c05";
            pkgs.attic-client;
          defaultPackages =
            (with pkgs; [
              bazel_9
              buildifier
              buf
              go
              nodejs_22
              pnpm
              protobuf
              python314
              python314Packages.mkdocs
              python314Packages.pyyaml
              ruff
              uv
            ])
            ++ rust.packages;
          infraValidationPackages =
            with pkgs;
            [
              conftest
              kubeconform
              kubernetes-helm
              kustomize
              python314
              python314Packages.pyyaml
              yamllint
              yq-go
            ]
            ++ [ promtoolBinary ];
        in
        {
          default = pkgs.mkShell {
            packages = defaultPackages;
            shellHook = standardShellHook;
          };
          # The full golang.org/x/tools command suite is useful interactively but contributes
          # hundreds of megabytes and is not used by repository automation. Keep it available
          # without charging every default shell for it.
          go-tools = pkgs.mkShell {
            packages = defaultPackages ++ [ pkgs.gotools ];
            # gotools carries the nixpkgs Go compiler as a propagated input. During a
            # security point-release overlay that compiler can otherwise precede the
            # repository's patched Go on PATH and silently build scanners against the
            # vulnerable standard library.
            shellHook = standardShellHook + ''
              export PATH="${pkgs.go}/bin:$PATH"
            '';
          };
          # CI lanes use the smallest closure that can execute their contract. The umbrella
          # `ci` shell remains the convenient interactive environment, but realizing it on a
          # fresh GitHub runner needlessly compiled Terraform and Prometheus for unrelated jobs
          # and exhausted the runner disk before the requested command started.
          ci-lint = pkgs.mkShell {
            packages = with pkgs; [
              actionlint
              buf
              python314Packages.mkdocs
              shellcheck
              yamllint
            ];
            shellHook = baseShellHook;
          };
          ci-terraform = pkgs.mkShell {
            packages = with pkgs; [
              conftest
              python314
              terraform
              terraform-docs
              tflint
              trivy
            ];
            shellHook = baseShellHook;
          };
          ci-infra = pkgs.mkShell {
            packages = [ pkgs.bazel_9 ] ++ infraValidationPackages;
            shellHook = standardShellHook;
          };
          ci-bazel = pkgs.mkShell {
            # The full Bazel test graph includes the Kubernetes/GitOps validation targets, so
            # their host-tool bundle is part of this lane even though configured compilation
            # uses Bazel-registered language toolchains.
            packages =
              (with pkgs; [
                bazel_9
                buildifier
                git
                python314
                # The full graph executes the DNS module's sandboxed Terraform test. Keep
                # Terraform in this lane only; Kubernetes/GitOps static validation does not
                # require the larger Terraform closure.
                terraform
              ])
              ++ infraValidationPackages;
            shellHook = trustedGitShellHook;
          };
          nix-cache-publisher = pkgs.mkShell {
            packages = [
              atticClient
              pkgs.git
              pkgs.nix
              pkgs.python314
            ];
            shellHook = baseShellHook;
          };
          ci = pkgs.mkShell {
            # actionlint/shellcheck/yamllint feed the `lint` lane, terraform the `terraform`
            # lane. Both sets of configs (.yamllint.yaml, .github/actionlint.yaml) existed in
            # the estate's other repositories with nothing running them; here they did not
            # exist at all, and neither did a Terraform gate over infra/ — which
            # infrastructure-live consumes as a module source, so a broken module there breaks
            # a different repository's plan.
            packages =
              with pkgs;
              [
                actionlint
                bazel_9
                buildifier
                buf
                cargo-deny
                conftest
                git
                go
                go-containerregistry
                kubeconform
                kubernetes-helm
                kustomize
                nodejs_22
                pnpm
                protobuf
                promtoolBinary
                python314
                python314Packages.mkdocs
                python314Packages.pyyaml
                ruff
                shellcheck
                syft
                terraform
                terraform-docs
                tflint
                trivy
                uv
                yamllint
                yq-go
              ]
              ++ rust.packages;
            shellHook = trustedGitShellHook;
          };
        }
        // nixpkgs.lib.optionalAttrs (system == "x86_64-linux") {
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
            packages =
              (with pkgs; [
                bazel_9
                buildifier
                go
                protobuf
                promtoolBinary
                python314
                ruff
                uv
              ])
              # `cccl`, not `cuda_cccl`: nixpkgs renamed it and the old attribute is an alias that
              # prints a deprecation warning on EVERY evaluation of this flake, including the ones
              # `nix flake check` does in CI. Aliases in nixpkgs are removed a release or two after
              # they start warning, so the warning is the notice period.
              ++ (with pkgs.cudaPackages; [
                cuda_nvcc
                cuda_cudart
                cccl
                cudnn
                nccl
                libcublas
              ])
              ++ rust.packages;
            shellHook = standardShellHook + ''
              # nvcc finds the headers and the stub libraries through CUDA_PATH. Set here rather
              # than left to the caller because a missing CUDA_PATH produces "cannot find
              # -lcudart" at link time, which reads like a broken build file.
              export CUDA_PATH="${pkgs.cudaPackages.cuda_nvcc}"
              export CUDA_HOME="$CUDA_PATH"
            '';
          };
        }
      );

      checks = forAllSystems (
        { pkgs, rust, ... }:
        import ./tools/build/nix/checks/default.nix {
          inherit pkgs versions;
          rustToolchain = rust.toolchain;
          root = self;
        }
      );

      # The regeneration side of checks/toolchain-manifest.nix. That check compares the
      # committed manifest against what the flake resolves; this is how you produce a new
      # committed manifest when the closure moved on purpose:
      #
      #   nix build .#toolchain-manifest
      #   install -m 0644 result tools/build/nix/toolchain-manifest.json
      #
      # Exposed as a package rather than hidden inside the check so the evidence the ADR calls
      # for is buildable on its own, without running the whole check set.
      packages = forAllSystems (
        { ccToolchain, pkgs, ... }:
        let
          manifest = import ./tools/build/nix/manifest.nix {
            inherit pkgs versions;
            root = self;
          };
        in
        {
          cc-toolchain-bundle = ccToolchain;
          toolchain-manifest = manifest.file;
        }
        // nixpkgs.lib.optionalAttrs pkgs.stdenv.hostPlatform.isLinux {
          remote-execution-base =
            (import ./tools/build/nix/images/default.nix {
              inherit ccToolchain pkgs;
              system = pkgs.system;
            }).cpu;
        }
        // nixpkgs.lib.optionalAttrs (pkgs.system == "x86_64-linux") {
          workstation-gce-image = workstationSystem.config.system.build.googleComputeImage;
          workstation-gce-image-contract =
            workstationSystem.config.environment.etc."mindclade/image-contract.json".source;
        }
      );

      # nixfmt-tree, not bare nixfmt, and not nixfmt-rfc-style.
      #
      # nixfmt-rfc-style is an alias that now warns on every evaluation — including the ones
      # `nix flake check` performs in CI — so it goes on principle.
      #
      # The wrapper rather than the formatter itself is the load-bearing part. `nix fmt` passes
      # its path argument straight to bare nixfmt, and `nix fmt .` therefore walks EVERYTHING
      # under the checkout: .direnv/flake-inputs holds symlinks into /nix/store, so the run ends
      # in a Haskell backtrace from a write to a read-only path, having formatted nothing useful.
      # nixfmt-tree wraps nixfmt in treefmt, which respects .gitignore and so sees the repository
      # rather than the caches inside it.
      #
      # `nix fmt` formats; `nix fmt -- --ci` is the check the presubmit lint lane runs.
      formatter = forAllSystems ({ pkgs, ... }: pkgs.nixfmt-tree);
    };
}
