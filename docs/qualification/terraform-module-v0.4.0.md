# Terraform module v0.4.0 publication preflight

`v0.4.0` is a planned, unpublished module release. This procedure qualifies and
publishes the exact reviewed source commit; it does not authorize a Terraform plan or
apply. Run it from an isolated checkout of the protected `main` commit, never from a
dirty developer worktree.

## Required inputs

- the approved 40-character source commit;
- successful required checks for that exact commit;
- Platform and Security approvals recorded against one change ticket;
- the `infrastructure-live` checkout that will consume `v0.4.0`;
- an operator who can create a protected SSH-signed annotated tag, distinct from the approvers;
- a source-qualified signer identity and unexpired evidence digest in
  `infra/terraform/governance/module-release-authority.json`;
- read-back evidence that immutable releases are enabled for the monorepo; and
- the exact `terraform-module-release` protected environment applied from `github-config`.

Abort if the proposed tag already exists, the checkout is dirty, the commit differs
from the approved commit, any lockfile would change, or candidate and exact-ref
validation do not describe the same source tree.

## Source qualification

Record every command, exit status, tool version, source commit, and output digest in
the change evidence. Substitute the approved commit for `<source-sha>`.

```bash
git fetch --tags --force origin
test "$(git rev-parse HEAD)" = "<source-sha>"
test -z "$(git status --porcelain=v1)"
test "$(git tag -l v0.4.0)" = ""

nix flake check --no-update-lock-file
nix develop .#ci-terraform --command ci/terraform/check.sh fmt
nix develop .#ci-terraform --command ci/terraform/check.sh contracts
nix develop .#ci-terraform --command ci/terraform/check.sh validate
nix develop .#ci-terraform --command ci/terraform/check.sh lint
nix develop .#ci-terraform --command ci/terraform/check.sh security
nix develop .#ci-terraform --command ci/terraform/check.sh test
nix develop .#ci-terraform --command ci/terraform/check.sh docs
nix develop .#ci-terraform --command ci/terraform/check.sh compat
nix develop .#ci-bazel --command tools/dev/bazelw build //... \
  --nobuild --config=ci --lockfile_mode=error
nix develop .#ci-bazel --command python3 tools/analysis/run_architecture_checks.py
nix develop .#ci-bazel --command python3 tools/analysis/check_bazel_layers.py
```

From the sibling live repository, validate the candidate source without weakening the
released-ref gate. `CANDIDATE_MODULE_REF` is not optional: `validate-source-integration`
delegates to `validate-module-candidate`, whose first line exits 2 unless the variable is
set. The gate takes a ref rather than a worktree path because it snapshots that exact
commit and never reads dirty or uncommitted monorepo bytes — a run without it is a refusal,
not a looser check. Pass the same `<source-sha>` asserted above:

```bash
nix develop .#ci --command make validate-source-integration \
  MONOREPO=../mindclade-internal-monorepo \
  CANDIDATE_MODULE_VERSION=v0.4.0 \
  CANDIDATE_MODULE_REF=<40-character-lowercase-commit-sha>
```

The live repository's `docs/module-interface-contract.md` documents this same invocation,
and its `scripts/validate-production-contract.py` pins both the Makefile recipe and that
literal placeholder. Keep the two documents identical; a divergence here is what let this
procedure publish a command that could not run.

Create two normalized source archives independently and require matching SHA-256
digests. The archive is evidence; consumers continue to use the immutable Git tag.

```bash
git archive --format=tar --prefix=mindclade-internal-monorepo-v0.4.0/ \
  <source-sha> | gzip -n -9 > /tmp/mindclade-module-v0.4.0-a.tar.gz
git archive --format=tar --prefix=mindclade-internal-monorepo-v0.4.0/ \
  <source-sha> | gzip -n -9 > /tmp/mindclade-module-v0.4.0-b.tar.gz
shasum -a 256 /tmp/mindclade-module-v0.4.0-{a,b}.tar.gz
cmp /tmp/mindclade-module-v0.4.0-{a,b}.tar.gz
```

## Protected publication

Only after all source evidence and approvals refer to the same commit may the release
operator create and push the SSH-signed annotated `v0.4.0` tag. The signed tagger identity and
SSH key fingerprint must match the qualified source contract; GitHub verification alone is not
signer authorization. Never move or recreate a published tag.

From the Actions UI, dispatch `Terraform module release` on protected `main` with the existing tag,
the exact signer-evidence digest, the immutable-release evidence digest, and the approved Mindclade
pull request or issue URL. The workflow first binds itself to the current protected-main head, then
enters the delayed Security-reviewed environment after independent Release tag signing. It checks
out and qualifies the tag's exact peeled commit, repeats GitHub and SSH signature verification,
builds a schema-2 manifest and checksum, attests the manifest, creates a draft with the three
assets, and publishes only after the draft is complete. The final read-back must report
`isImmutable: true`.

The workflow uses `gh release create --verify-tag`; it has no command that can create, delete, push,
or move a tag. As of 2026-08-23 the connected repository reports immutable releases disabled and
the signer authority remains `blocked`, so dispatch is intentionally ineligible until those
operator gates are closed in reviewed source and live governance.

After publication, rerun the live repository's released-ref contract before any plan:

```bash
git fetch --tags --force origin
test "$(git rev-list -n 1 v0.4.0)" = "<source-sha>"
nix develop .#ci --command make validate-integration MONOREPO=../mindclade-internal-monorepo
```

The release remains unusable for production until the live estate also retains a
credentialed, policy-evaluated saved plan, cost and replacement review, required
approvals, rollback ownership, and connected non-production evidence. Publication does
not satisfy those gates and must not trigger an apply.

## Failure and rollback boundary

Before the signed tag exists, fix source defects and restart qualification from a new approved
commit. After the tag exists, never move or recreate it. A failed run may leave a draft for
operator inspection; resolve that draft only through the same protected change and Security
approval boundary. Retry the same tag only when its released source remains correct; otherwise
leave the tag unreleased and qualify a new patch version. After publication, do not delete or
retarget the tag, replace its assets, or edit its evidence; stop consumers, record the incident,
restore their previous immutable module ref, and issue a new patch release for any correction.
Terraform state rollback is not a module-source rollback.
