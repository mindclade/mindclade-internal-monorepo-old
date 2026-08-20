# GitHub configuration

## Workflows

| Workflow | Gates |
|---|---|
| `presubmit.yml` | The PR gate: architecture invariants, Go, Rust, Python, actionlint/yamllint, Terraform, Bazel loading, pnpm workspace integrity |
| `release.yml` | Image build, sign, and attest. The filename is load-bearing — bootstrap binds the attestor identity to this exact path |
| `security.yml` | CodeQL and the security lane. Holds the only `security-events: write` in the repository |
| `license-headers.yml` | The four-line header every source file carries |

`gpu.yml` and `nightly.yml` are reserved by the blueprint and deliberately not
written. They need self-hosted runners and a release process that do not exist
yet, and an empty workflow that reports success is worse than an absent one.

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
build logic.

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
