# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

`AGENTS.md` is the operating guide and binds here — read it. This file adds the commands and the
enforcement mechanisms it does not carry.

## Environment

Never install tools into the host to make a command work, and never duplicate a language
dependency graph in Nix. Two wrappers are the entry points — call these, not bare `nix` or
`bazel`:

```sh
tools/dev/nixw develop .#default          # dev shell (also via `direnv allow`)
tools/dev/bazelw <bazel args>             # verifies the launcher against .bazelversion
```

Shells: `.#default` (day-to-day; already has buildifier and mkdocs), `.#go-tools` (adds the
`golang.org/x/tools` suite), `.#ci` (umbrella), the narrow lanes `.#ci-lint`, `.#ci-bazel`,
`.#ci-terraform`, `.#ci-infra`, and `.#gpu` — x86_64-linux only, for *compiling* TileLang
kernels; it is not what runs torch. `flake.nix` owns the contents; check there before assuming
a tool is absent and reaching for a heavier shell.

## Commands

### Start here for any change

```sh
python3 tools/analysis/run_architecture_checks.py                  # fastest inner loop
tools/dev/nixw develop .#ci --command python3 ci/presubmit/pipeline.py --static-only
tools/dev/nixw flake check --no-update-lock-file
```

`--static-only` runs every checker in the `CHECKS` list of
`tools/analysis/run_architecture_checks.py` and returns before any Bazel work. Those checkers are
stdlib-only, so the bare `python3` form above needs no Nix and no installed deps.

Then run the affected graph rather than `//...`:

```sh
tools/dev/nixw develop .#ci-bazel --command python3 ci/presubmit/pipeline.py \
  --bazel-only --mode affected --base <base-sha>
```

`ci/common/affected.py` computes the owning package plus reverse dependencies from Bazel's
post-loading graph, and forces full-graph mode itself for CI, toolchain, dependency-lock,
Starlark, protocol, architecture, component, and maturity-policy inputs — don't re-derive that
rule, and don't reach for `//...` by hand.

### Bazel

```sh
tools/dev/bazelw test //libs/go/retry:retry_test --config=ci   # one target
tools/dev/bazelw test //kernels/... --config=ci                # one subtree
tools/dev/bazelw build //... --nobuild --config=ci             # configured analysis only
tools/dev/bazelw query '//...' --config=ci --output=label      # every BUILD loads
```

`--config=ci` adds `--lockfile_mode=error`, `--test_tag_filters=-manual`, and scaled timeouts.
`--config=hermetic` restricts fetches to `bazel_downloader.cfg`. `user.bazelrc` is gitignored and
try-imported for local overrides.

### Go

```sh
go test -race -count=1 ./libs/go/... ./control/... ./services/control_plane/... ./examples/go/...
go test -race -count=1 ./control/routing                            # one package
go test -race -run TestSnapshotPublicationMonotonic ./control/routing
go vet ./libs/go/...
tools/qualification/go/validate.sh offline
```

`-run` with a name that matches nothing exits **0** and prints `[no tests to run]` — it does not
fail. Read the output, not the exit code. PostgreSQL-backed suites skip without
`MINDCLADE_TEST_POSTGRES_DSN`. Go is pinned to 1.26.6 in `flake.nix`; CI runs `GOTOOLCHAIN=local`.

### Rust

```sh
cargo test --workspace --all-targets --all-features --locked
cargo test -p mindclade_runtime_core budget                    # crates are all mindclade_*
cargo clippy --workspace --all-targets --all-features -- -D warnings
cargo fmt --all -- --check
cargo deny check
python3 tools/qualification/rust/qualify.py --mode presubmit    # canonical Rust presubmit
```

`Cargo.toml`'s `workspace.package.rust-version` (1.97.1) is the authority, and
`tools/analysis/check_build_toolchain_contract.py` enforces that `rust-toolchain.toml`,
`tools/build/nix/versions.nix`, `MODULE.bazel`'s `rules_rust` versions, and
`tools/qualification/rust/common.py` all agree. Bumping Rust means editing all of them together.

### Python

```sh
uv run --frozen pytest                                        # default run excludes `nightly`
uv run --frozen pytest libs/python tests/integration/cross_language
uv run --frozen pytest models/families/llm/tests/test_llm.py::test_causal_prefix_is_invariant_to_future_tokens
uv run --frozen pytest -m nightly                             # target-state progress tests
uv run --frozen ruff check . && uv run --frozen ruff format --check .
uv run --frozen --only-group typecheck mypy libs/python       # strict mypy, the repo's checker
```

Python 3.14; ruff `target-version = py313` because presubmit checkers are imported by the
bootstrap host before Nix establishes the 3.14 runtime. pytest uses `--import-mode=importlib` and
`--strict-markers` — only `nightly` and `scaffold` are declared.

### TypeScript

```sh
pnpm lint          # check_typescript.py, then builds sdk + libs/ts, then per-package lint
pnpm typecheck
pnpm test
pnpm generate:check
```

### Lint, docs, infra

```sh
tools/dev/nixw develop .#ci-lint  --command buf lint protocols
tools/dev/nixw develop .#ci-lint  --command yamllint --strict .
tools/dev/nixw develop .#ci-lint  --command actionlint -color
tools/dev/nixw develop .#ci-lint  --command mkdocs build -f docs/mkdocs.yml --strict
tools/dev/nixw develop .#ci-bazel --command buildifier --mode=check -r .
tools/dev/nixw develop .#ci-terraform --command ci/terraform/check.sh all
python3 tools/dev/validate_repository.py
```

`ci/terraform/check.sh` dispatches on `$1` only — pass `all`, or one of
`fmt|contracts|validate|lint|security|test|docs|compat|plan-policy`. A brace-expanded list runs
the first mode and silently drops the rest.

## Architecture

### Language law

Crossing these boundaries requires an ADR, not convenience:

```text
Go          fleet control plane and durable policy
Rust        online/runtime data plane and node execution
Python      scientific, model, training, inference, and evaluation numerics
TileLang    qualification-gated accelerator kernels (always behind a fallback)
TypeScript  browser applications and generated web clients
```

### Bazel layer matrix

`tools/build/bazel/layers.bzl` is the machine-readable source of truth — read it before
classifying anything. It defines **13** domains, and every internal package must match exactly
one; unclassified or doubly-classified fails immediately:

```text
foundation  offline  training  training_service  runtime  services  apps  research
platform  build_support  release_support  test_support  root_support
```

The last five matter more than they look: `tools/`, `ci/`, `tests/`, `docs/`, and `infra/` — the
directories you will touch most — are classified there, not unclassified. A new top-level code
package needs an entry here plus `OWNERS.toml` and `.github/CODEOWNERS`.

Flow: `protocols -> generated bindings/libs -> data/preprocessing/kernels/models ->
training/serving/evaluation -> services -> apps (through SDKs and contracts only)`. Production
may never import `research/`; `research/` may import production. Consult
`BAZEL_LAYER_ALLOW_MATRIX` for a specific edge rather than guessing from the flow — `apps`, for
instance, reaches six domains, not two.

The matrix is fail-closed: an unlisted direction is forbidden. See
`docs/architecture/dependency-rules.md` for the exception schema and its full conditions — a
`BAZEL_LAYER_EXCEPTIONS` entry that omits any of them fails CI.

### Go layers inside `libs/go`

`libs/go/LAYERS.md` is authoritative for which package sits where. The rule that matters while
editing: **lower never imports higher**, and `libs/go` never imports `control/`, `services/`,
`data/`, models, or executables.

```text
0 foundations · 1 stable contracts · 2 runtime + durable coordination
3 provider adapters · 4 transports · 5 consumers (outside libs/go)
```

- Every production process assembles through `libs/go/servicekit/production.Builder` (layer 2).
- Domain policy in `control/`; deployable wiring in `services/`. A process entry point holds
  configuration, provider construction, registration, and exit policy — not domain algorithms.
- Check `libs/go/LAYERS.md` for an existing mechanism before writing one service-locally;
  `libs/go/USAGE.md` shows the idiom for each.
- **Adding a new top-level `libs/go/*` package is a closed-allowlist change.**
  `libs/go/ADMISSION.toml` carries `allowed_top_level`; any new directory with `.go` files is
  rejected until it is added there. `forbidden_names` bans 13 names — `common`, `shared`,
  `helpers`, `utils`, `platform`, `workflow`, `repository`, `state_machine`, `queue`, `tenant`,
  `quota`, `runs`, `jobs`. Admission also requires two independent consumers
  (`libs/go/ADMISSION.md`).
- Two Go modules exist and both are legitimate: root `go.mod` (`go.mindclade.dev`) and the public
  `sdk/go/go.mod`. `check_go_modules.py` allows exactly those two — nested modules under
  `libs/go` are rejected, and root `go test ./...` does not cover `sdk/go`.

### Rust crate consolidation

`clock`, `retry`, `resource_version`, `observability`, `artifact_manifest`, `byte_spec`, and
`python_bindings` are **removed**, not deprecated — they are gone from `libs/rust/`, from the
workspace members, and from `Cargo.lock`. Canonical implementations live in `runtime_core`,
`telemetry`, `manifests`, `bytes_io`, and `python_bridge`.
`tools/analysis/check_code_docs_alignment.py` fails if a `libs/rust/<name>` directory reappears
**or** if any `.rs`/`Cargo.toml` under `libs/rust` references `mindclade_<name>` — that second
clause is the one you can trip by copying an old import.

(`docs/architecture/dependency-rules.md` still describes these as live facades. It is stale on
this point; the checker is authoritative.)

### Maturity model — read before depending on any path

The tree is a target-state scaffold with real implementations mixed in. `components.toml` records
each component's status; `maturity.toml` declares the seven statuses and the gates each one must
satisfy — it is the source of truth, and adding a gate there will not update this file.

```text
planned / scaffolded / experimental   -> production may NOT depend on these
implemented / qualified / production  -> progressively more evidence required
deprecated                            -> retirement
```

Advancing a status additionally requires an owner in `OWNERS.toml`. Never convert a stub or a
skipped test into a readiness claim. `REPOSITORY_STATUS.md`, `VALIDATION.md`, and
`QUALIFICATION.md` record what is actually proven.

### Contracts and generated code

Schema changes start in `protocols/`, regenerate via `tools/codegen/*`, then run compatibility
tests. Anything marked `linguist-generated` in `.gitattributes` is generated — never hand-edit it.

### Test placement

Unit and component tests are colocated with their package. Top-level `tests/` holds only
cross-package, cross-process, cross-language, device, numerical, performance, resilience, scale,
and security qualification.

## Conventions

- Commits: Conventional Commits with a scope — `fix(ci): restore main formatting gates (#60)`.
  Work happens on branches; `main` takes pull requests only.
- Every source file opens with the three-line proprietary header;
  `python3 tools/analysis/check_license_headers.py --fix` inserts it.
  `COMMENT_PREFIX` in that file is the authoritative extension list.
- **Never weaken, skip, or allowlist a gate to make a change pass.** The `manual` test tag and
  the `scaffold` marker are *not* escape hatches — reaching for either to get green is precisely
  the violation. The three sanctioned routes are a time-boxed `BAZEL_LAYER_EXCEPTIONS` entry, an
  ADR in `docs/design/decision-register.md`, or moving a ratchet with a comment saying what
  changed. `MATERIALIZATION_BASELINE` (`tests/integration/test_blueprint_scaffold.py`) is
  asserted `== 0`, so it moves only if the manifest gains a path; `SCAFFOLD_BASELINE`
  (`tests/integration/test_python_scaffold.py`) may be lowered.
- Every queue, parser, buffer pool, retry loop, and spool must be bounded.
- Never commit credentials, kubeconfigs, model weights, private datasets, holdout data, patient
  or partner data, or local caches. Treat all external biological files and model inputs as
  untrusted.
- Comments here explain *why* a decision holds and what the prior defect was. Match that density
  and voice when editing a file that has it.

## Further reading

`docs/architecture/system-design-reference.md` (canonical design) ·
`docs/architecture/dependency-rules.md` · `docs/design/decision-register.md` (ADRs) ·
`libs/go/LAYERS.md` + `libs/go/USAGE.md` · `docs/guides/go-service-golden-path.md` ·
`docs/guides/component-maturity-and-dependency-budgets.md` · `docs/runbooks/`
