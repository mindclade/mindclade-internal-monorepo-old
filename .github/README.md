<p align="center">
  <a href="../README.md"><img src="assets/brand/mindclade-wordmark.png" alt="Mindclade" width="240"></a>
</p>

[← Repository home](../README.md) · [CI lanes](../ci/README.md) · [Validation](../VALIDATION.md)

# GitHub configuration

> **Maturity:** Presubmit and security workflows are active repository contracts. The ARC
> release path is source-complete but remains fail-closed until its connected activation gates
> pass; Buildkite is retained only as non-authoritative rollback evidence during that rollout.

## Workflows

| Workflow | Gates |
|---|---|
| `presubmit.yml` | The PR gate: architecture invariants, Go, Rust, Python, actionlint/yamllint, Terraform, affected Bazel configured analysis/tests with fail-closed full fallback and mandatory Gazelle qualification, pnpm workspace integrity |
| `nightly.yml` | Daily/manual CPU qualification of the complete configured Bazel graph and all non-manual tests |
| `release.yml` | Image build, sign, and attest. The filename is load-bearing — bootstrap binds the attestor identity to this exact path |
| `security.yml` | CodeQL and the security lane. Holds the only `security-events: write` in the repository |
| `license-headers.yml` | The four-line header every source file carries |

`gpu.yml` remains reserved because it requires qualified self-hosted hardware.
The CPU-only nightly is intentionally hosted and makes no GPU, provider,
remote-execution, release, or deployment claim.

## The `ci` job id is load-bearing

`github-config`'s `required-checks-go` ruleset requires the status check context
`ci / build`. For a called reusable workflow GitHub composes that context as
`<caller job id> / <called job id>`, and `reusable-go-ci`'s job is `build` — so the
Go lane's job **must** stay `ci`. Renaming it produces a required check that is
never satisfied, and a default branch nothing can merge into.

`github-config` also declares the stable `architecture` context. The `lint` and
`terraform` jobs now use stable ids and display names, but remain staged until a
real pull request confirms their exact emitted contexts; the separate ruleset
change then makes them merge-blocking without risking an unsatisfiable branch.

The Bazel job emits the exact `bazel / verdict` context on pull requests,
merge groups, and protected-main pushes. Its governance rule remains in
evaluate mode until successful and intentional-negative connected canaries are
reviewed. Renaming it requires an evaluate-mode context migration first.

## Shared workflow access is load-bearing

The Go, Rust, and Python jobs call SHA-pinned reusable workflows from
`mindclade/.github`. That repository's Actions access must remain
`organization`; `none` makes GitHub reject `presubmit.yml` during workflow
startup, before it creates a single job or useful log. Confirm the migration
contract with:

```bash
gh api repos/mindclade/.github/actions/permissions/access --jq .access_level
```

Repository moves require updating both the reusable-workflow owner and immutable
commit SHA. A redirect that works for `git fetch` does not make an old Actions
workflow reference valid.

## Relationship to `ci/`

Workflows decide *when*; `ci/{presubmit,gpu,nightly,release,security}/` decide
*what*. Each lane there is a `pipeline.py` plus a `targets.yaml`, so the target
selection is reviewable and runnable locally rather than embedded in YAML. The
architecture lane is `python3 ci/presubmit/pipeline.py --static-only`.

Bazel is the test execution authority. CI selects targets; it does not duplicate
build logic. Pull requests, merge groups, main pushes, and nightly runs currently
execute `//...`. Owning-package reverse-dependency selection and the separate
graph-native target-determinator migration remain blocked on connected cache and
external required-workflow evidence.

## Buildkite retirement status

The `.buildkite/` source remains temporarily for audit and rollback comparison, but bootstrap
now rejects any enabled Buildkite provider and normal-plane IAM grants it no authority. Its
checkout verification cannot establish canonical protected-main ancestry for API/custom-refspec
builds, so it must never be reactivated. Remove the dormant source only after two connected ARC
releases and the documented rollback drill pass.

## Release activation status

`release.yml` accepts only a protected-main push that adds exactly one immutable reviewed
request under `ci/release/requests/`. It calls the immutable `.github` v4 ARC canary, builder,
independent qualifier, protected deployment signer, and review-only GitOps promoter. The source
remains inactive until `.github` v4 is published, the exact runner group and GitHub Apps exist,
the six WIF capabilities are applied and negatively tested, and the connected canary passes.

## Dependency updates

There is no `dependabot.yml`. Dependabot is disabled org-wide in `github-config`
and Renovate owns dependency updates for the estate — see `renovate.json5`, which
covers Cargo, Go, pnpm, uv, and Nix locks. Adding a Dependabot config here would
field a second updater against the same lockfiles.

## Lint configs

`.yamllint.yaml` and `.github/actionlint.yaml` at the repository root are the
canonical copies; the `.github` repository's are copies of these. Both run from
the pinned Nix `.#ci` shell so a local run and the lane use the same binaries —
actionlint shells out to shellcheck, and versions disagree about SC2153 and SC2015.
