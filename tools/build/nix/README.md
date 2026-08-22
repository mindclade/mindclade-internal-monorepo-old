# Tools / Build / Nix

- **Status:** Source implementation complete for the exported native toolchain surface;
  connected Linux/remote-execution qualification remains a promotion gate.
- **Owner:** `@mindclade/platform` through the repository ownership catalog.
- **Decision:** [ADR-0002](../../../docs/design/adr-0002-nix-owned-toolchains.md).

## Implemented surface

`flake.nix` and `flake.lock` are the public interface. They export native shells, checks,
the normalized C/C++ bundle, and the toolchain manifest for `x86_64-linux`,
`aarch64-linux`, and `aarch64-darwin`. Linux systems additionally export the reproducible,
non-root `remote-execution-base` OCI archive. Bazel 9.1.1, the language toolchains, and CI tools
come from the pinned Nix closure; `.bazelversion` remains a verified compatibility pin.

The enterprise control repositories use the stable `nixos-26.05` branch. This monorepo has a
narrow, reviewed exception: its lock pins one immutable `nixos-unstable` commit because the
current 26.05 revision supplies Bazel 9.1.0 while the build graph is qualified on the 9.1.1 LTS
patch. The exact Nixpkgs revision and resolved package versions are committed in the manifest,
so evaluation never follows a moving channel. Reconsider the exception when 26.05 carries the
required patch or during the 26.11 release qualification; do not add a second Nixpkgs input to
work around individual packages.

The active implementation is intentionally small:

- `versions.nix`, `manifest.nix`, and `toolchain-manifest.json` own reviewed version evidence;
- `toolchains/cc.nix` and `toolchains/rust.nix` construct the normalized compiler closures;
- `checks/` rejects lock, version, manifest, generated-file, and host-tool drift;
- `images/cpu.nix` and `lib/mk-exec-image.nix` construct the AMD64/ARM64 Linux action base
  with uid/gid 65532, an isolated writable `/tmp`, and no host-tool fallback;
- the root flake assembles minimal lane-specific shells and exports the C/C++ bundle consumed
  by `tools/build/bazel/extensions/nix_toolchains.bzl`.

The other target-state files under `bundles/`, `platforms/`, `shells/`, and `toolchains/`
remain explicit scaffolds and are not imported or exported. Their
presence in the blueprint is not evidence that those capabilities exist.

## Authority and unsupported outputs

This repository owns developer/CI toolchains and remote-execution bases; Bazel owns build,
test, application OCI images, release bundles, and promotion. It does not own workstation or
server lifecycle, so it exports no NixOS, nix-darwin, or Home Manager configurations.

All current outputs are native. Cross compilation is not silently inferred from an evaluable
foreign-system attribute. `x86_64-darwin` is intentionally unsupported, and CUDA is exported
only for native `x86_64-linux`; ROCm and TileLang closures are not exported. The Linux OCI
archive is a Nix-owned action base, not a Buildfarm server or worker image and not a deployment
claim. Its registry digest remains an operator-supplied release input. Unsupported requests fail by absent attributes
rather than falling back to host tools.

## Qualification

Source qualification:

```bash
nix flake show --all-systems --no-write-lock-file
nix flake check --no-update-lock-file
nix develop --no-update-lock-file --ignore-environment .#ci-bazel --command \
  tools/dev/bazelw build //... --nobuild --config=ci
```

These commands do not prove remote execution, Linux runtime parity from a Darwin host, CUDA,
ROCm, cache availability, or production deployment. Those claims require connected evidence
from the exact native worker/runtime and remain blocked until that evidence is attached to a
release qualification record.
