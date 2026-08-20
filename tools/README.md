<p align="center">
  <a href="../README.md"><img src="../.github/assets/brand/mindclade-wordmark.png" alt="Mindclade" width="240"></a>
</p>

[← Repository home](../README.md) · [Contributing](../CONTRIBUTING.md) · [Validation](../VALIDATION.md)

# Repository tooling

> **Maturity:** Mixed; each tool documents its own supported and qualified
> surface.
> **Primary implementation:** Bazel, Nix, Python, Go, and Rust.

`tools/` contains repository-owned developer, build, code-generation,
analysis, qualification, licensing, and release utilities.

## What's here

| Path | Responsibility |
| --- | --- |
| [`dev/`](dev/) | Repository-aware Nix and Bazel entry points |
| [`build/`](build/) | Toolchains, build metadata, and packaging support |
| [`analysis/`](analysis/) | Architecture, maturity, dependency, and repository invariant checks |
| [`codegen/`](codegen/) | Reproducible generation entry points |
| [`qualification/`](qualification/) | Go, Rust, kernels, security, scale, and GKE qualification |
| [`release/`](release/) | Release assembly and evidence helpers |
| [`license/`](license/) | License-header and compliance tooling |

## Boundary

- Production and CI paths invoke repository-owned Bazel targets or checked-in
  wrappers; ad hoc host tooling is not an authority.
- Generated output must have a reproducible source and command.
- Tools do not silently choose a release, environment, or production identity.
- A helper shared by product code belongs in the appropriate language library,
  not in this directory.

## Start here

- [`tools/dev/nixw`](dev/nixw) applies the repository Nix configuration.
- [`tools/dev/bazelw`](dev/bazelw) enforces the pinned Bazel launcher.
- [`tools/qualification/README.md`](qualification/README.md) indexes
  qualification lanes.
- [`CONTRIBUTING.md`](../CONTRIBUTING.md) defines the expected change workflow.
