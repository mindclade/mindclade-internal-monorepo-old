# Build and toolchains

## Decision

Bazel with Bzlmod owns the build graph. Nix owns pinned toolchains and execution
environments. They do not compete for dependency or release authority.

## Bazel owns

- build analysis and compilation;
- tests, code generation, linting, and affected-target selection;
- OCI application images and release bundles;
- numerical, performance, safety, security, and scale qualification;
- SBOM, provenance, signatures, and promotion targets;
- local/remote execution platform selection.

## Nix owns

- Bazel, Go, Rust, Python, Node, C/C++, CUDA/ROCm, Protobuf, and docs tools;
- developer and CI shells;
- normalized toolchain bundles and manifests;
- system/runtime closures and remote-execution worker bases;
- trusted binary caches.

```text
flake.nix + flake.lock
  -> Nix toolchain closure + manifest
  -> Bzlmod extension and registered toolchains
  -> Bazel build/test/package/image/qualify/release

same Nix derivation
  -> immutable remote worker/base image
  -> digest verified against the manifest
```

## Rules

- Bzlmod only; no legacy dependency graph;
- one root module/workspace per language unless independently published;
- no host tool or package installation leakage;
- the same Nix derivations back local and remote toolchains;
- Nix cache, Bazel CAS, and platform artifact CAS remain separate;
- production OCI images are Bazel outputs built from Nix-produced bases;
- generated compatibility version files are verified, not independently owned.

## Developer path

```bash
tools/dev/nixw develop .#default --command tools/dev/bazelw test //...

tools/dev/nixw develop .#gpu --command tools/dev/bazelw test --config=cuda //tests/...

tools/dev/nixw develop .#default --command mkdocs build -f docs/mkdocs.yml --strict
```

`tools/dev/bazelw` is the repository entry point. The Nix shells provide Bazel 9.1.1 as a
fixed-output package from the pinned Nixpkgs revision; the wrapper verifies that exact binary
before use and accepts Bazelisk only as a compatibility fallback outside Nix. It moves execution
to the workspace root and passes arguments unchanged. It does not discover compilers, inject
Darwin flags, or choose a configuration or target set.

Repository traversal policy is split by syntax: `.bazelignore` owns literal
root tool-output paths, while `REPO.bazel` uses Bazel 8+ glob semantics for
nested generated trees. Node dependency trees, Python bytecode/tool caches, and
Terraform provider caches stay out of `//...`; committed sources and lock files
remain visible to Bazel governance targets.

The root Python environment has separate hash-locked Linux and arm64 Darwin requirements
files. PyTorch's CPU index publishes different local-version metadata on those
platforms, so feeding a universal export to rules_python can associate a Linux wheel
with Darwin hashes. `MODULE.bazel` maps each generated lock to its supported target
platform and uses rules_python's package-scoped index override: PyPI remains authoritative
for transitive packages, while only Torch can resolve from the CPU index. The generated
locks therefore contain no global extra-index option. `uv.lock` continues to own developer
resolution, and the Linux lock is the independent dependency-audit input.

The Bazel interpreter resolves the `3.14` language selector to the exact Python patch recorded
by the Nix toolchain manifest and reusable CI. Because rules_python's built-in minor mapping can
lag security patch releases, `MODULE.bazel` registers the upstream python-build-standalone
archives for Linux amd64, Linux arm64, and Darwin arm64 with per-platform SHA-256 digests. The
build-toolchain contract fails if the patch mapping, supported platform set, archive origin, or
checksums are removed; Bazel tests therefore cannot silently fall back to an older `3.14.x`.

Every development and CI shell exports `MINDCLADE_CC_TOOLCHAIN_ROOT` from
`packages.<system>.cc-toolchain-bundle`. The bundle records Clang and binutils,
resource headers, target triple, platform constraints, system include paths,
SDK/sysroot, compile/link flags, and the Darwin deployment target. The
`nix_toolchains` Bzlmod extension validates that manifest and registers
`@mindclade_nix_cc` for the host constraints. Missing tools, headers, or a
Darwin SDK fail repository configuration with a focused diagnostic.

On Darwin, the shell also provides a Nix-owned `xcode-select` compatibility
adapter for rules_python repository materialization. It exposes a
CommandLineTools-shaped path whose SDK is a symlink to the pinned Nix SDK; it
does not select the C/C++ compiler. Compile and link actions resolve through the
registered toolchain and never through Command Line Tools or Homebrew.

The root Bzlmod module also declares the rules_rust compiler version and Rust
2024 edition explicitly. Root extension tags take precedence over rules_rust's
transitive default, so a rules_rust update cannot silently choose a different
compiler. The build-toolchain contract requires that Bazel version to equal the
Cargo and Nix pin; `MODULE.bazel.lock` binds its downloaded artifacts. Replacing
this parity bridge with the Nix Rust derivation remains part of local/remote
toolchain unification and is required before release qualification can claim
one physical Rust closure across both build paths.

Validate the contract and resolution directly:

```bash
tools/dev/nixw develop .#ci-bazel --command \
  python3 tools/analysis/validate_cc_toolchain_bundle.py
tools/dev/nixw develop .#ci-bazel --command tools/dev/bazelw test \
  //tools/build/bazel/toolchains/cc:smoke_test --config=ci
tools/dev/nixw develop .#ci-bazel --command \
  python3 tools/analysis/verify_cc_toolchain_selection.py
```

The committed Bzlmod lock is enforced read-only by `--config=ci`. The standalone layering and
toolchain-selection checkers also pass `--lockfile_mode=error` themselves so invoking them
cannot repair drift before the CI configuration sees it. After an intentional module or
extension change, regenerate and verify the lock explicitly:

```bash
tools/dev/nixw develop .#ci-bazel --command \
  tools/dev/bazelw mod deps --lockfile_mode=update
tools/dev/nixw develop .#ci-bazel --command \
  tools/dev/bazelw build //... --nobuild --config=ci
```

Every presubmit loads all BUILD files, checks the language-independent
dependency graph, and validates the registered toolchain. Protected pull
requests use `ci/common/affected.py` to map changed files to owning-package
seeds and ask Bazel for `rdeps(//..., seeds)`. Bazel's post-loading graph—not a
BUILD-file regex—selects rules and tests, while mandatory Gazelle qualification
remains in every non-empty affected test set.

CI, Starlark, toolchain, dependency-lock, protocol, architecture, component,
maturity, deletion, rename, and unmapped changes expand to `//...`. Merge-group,
protected-main, and nightly events always use `//...`; pull requests use affected
selection only when the diff and Bazel graph are authoritative.

Analysis and test phases emit a JSON Build Event Protocol stream, compressed
trace profile, normalized summaries, versioned selection record, exact target
files, and run metrics retained for 35 days. The affected-pull-request 30-minute
p95 objective qualifies after a 28-day burn-in of retained live samples. Release and remote-execution
claims still require separate platform evidence.

## Go module checksum closure

Internal Go code has one root `go.mod` and one root `go.sum`. The committed
`go.sum` must authenticate every direct public requirement and its `go.mod`.
The connected Go qualification lane then executes:

```text
go mod download all
go mod verify
go mod tidy -diff
```

This deliberately separates **committed dependency identity/checksums** from
**network/provider qualification**. A sandbox that cannot reach the production
module mirror may still validate the direct checksum seed and all offline-safe
packages, but only the connected pinned environment can certify complete
transitive source availability and a tidy module graph.
