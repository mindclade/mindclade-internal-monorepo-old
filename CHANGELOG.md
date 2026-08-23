<!-- mindclade-doc: changelog@1 -->

# Mindclade changelog · internal monorepo

All notable changes to the repository architecture and released implementation
surfaces are recorded here. Individual model, dataset, runtime, and service
releases also carry immutable release manifests and evidence bundles.

## 2026-08-23 — Terraform module release publication source

- replaced tag-push publication with a protected-main manual dispatch that consumes an existing
  signed annotated tag and has no tag creation, movement, deletion, or repair path;
- added a fail-closed source authority for an exact SSH signing-key fingerprint, signed tagger
  identity, expiring signer evidence, and independent immutable-release evidence;
- qualified the tag's exact peeled source before building and attesting a schema-2 module manifest,
  then attached the manifest, checksum, and attestation to a draft before immutable publication;
- reauthorized current `main`, the exact tag, fresh owner-enforced immutability evidence, and all
  three asset digests after the environment wait and again at the draft publication boundary;
- kept v0.4.0 planned and the authority blocked until the signer, immutable-releases setting,
  protected Security-reviewed environment, independent Release tag signing, and current whole-tree
  qualification are proven; and
- corrected the readiness inventory to the generated 47-module interface manifest.

## 2026-08-23 — Control orchestration and scheduling

- implemented `control/orchestration` — workflow compilation, the stage dependency graph, the
  stage and attempt state machines, leases, cancellation propagation, and the executor — in
  place of the ten `const scaffold_*` files that had held the boundary, and advanced the
  component from `experimental` to `implemented` on the tests that replaced them rather than by
  editing the status;
- implemented `control/scheduling` — quota admission, fair-share priority, placement, topology,
  pools, reservations, and preemption — and declared it in `components.toml` for the first time,
  because a reserved boundary is neither pass nor fail to the maturity gate, which is worse than
  a bad status since nothing can report on it;
- paired both with tier-1 entries in `architecture/component_ownership.toml`, an SLO, and a
  runbook, and recorded the correctness invariants that are not tradable for availability: no
  commit under a stale fencing token, no attempt beyond the declared budget, quota conserved
  across admission/reservation/release, and no accelerator reservation held while an upstream
  preprocessing stage is still pending;
- implemented the Kubernetes, Slurm, and local launchers behind one `Launcher` contract and
  added `control/orchestration/launchertest`, a single conformance suite all three run, because
  three adapters each testing its own happy path is how duplicate delivery, a late cancellation,
  and a superseded fence come to mean three different things across three providers;
- pinned the Go and Rust attempt transition tables against each other and against the
  `WorkerState` enum in the runtime protocol; each language had been asserting its table against
  a copy of itself, so a transition added on one side and not the other built clean in both, and
  the first symptom would have been a stuck run — a control plane rejecting a status its worker
  legitimately sent, or advancing an attempt the worker will never report reaching;
- recorded ADR-0026 for the three boundaries this work had to decide: the attempt-state
  vocabulary, what `control/runs` owns against what `control/orchestration` owns, and
  single-writer ownership of the Kueue and JobSet objects the blueprint had listed under both
  packages, which is the split-brain shape fencing exists to prevent everywhere else; and
- retired two stale waivers — the `cursortest` and `workqueuetest` entries in
  `libs/go/UNCONSUMED.toml`, cleared by having the memory adapters run those conformance suites
  as the waiver text itself asked, and the dated `control/ingestion -> control/orchestration`
  exception in `tools/analysis/check_production_dependencies.py`, whose own text named this
  component advancing on its own evidence as one of its two honest resolutions and ruled out
  editing a status until the check passed.

Evidence is offline and single-host: `go test -race` over both packages and the three launcher
adapters, plus the cross-language transition test. The Kubernetes launcher ran against the
`controller-runtime` fake client and the Slurm launcher against an in-memory controller fake;
`control/scheduling/adapters/jobset` and `control/scheduling/adapters/kueue` carry no test files
yet, and `[no test files]` is not a pass. No connected CI run, real Kubernetes or Slurm cluster,
Kueue or JobSet admission path, PostgreSQL-backed repository, or GPU measurement backs either
component. Both are `implemented`, which is neither `qualified` nor `production`.

## 2026-08-23 — Private developer workstation module

- added the reusable `workstation` module: one `x86_64-linux` instance with no external
  address, reachable only through IAP TCP forwarding, carrying a persistent CMEK data disk
  that keeps `/nix` and the Bazel disk cache across a stop;
- bound all three grants a tunnel actually needs — `roles/iap.tunnelResourceAccessor`, OS
  Login, and `roles/iam.serviceAccountUser` on the instance identity — because omitting the
  third satisfies IAM and then fails at connect, which presents as a broken tunnel and sends
  the operator to debug the wrong layer;
- kept the IAP source range `35.235.240.0/20` a local rather than an input, refused Arm and
  unquotaed `c3d-*` machine types, and refused `/nix` on ephemeral Local SSD, since a
  configurable source range is how a rule that exists to admit only IAP eventually admits
  `0.0.0.0/0`;
- limited the workstation identity to the observability floor and denied KMS signing, Binary
  Authorization attestation, container analysis, Artifact Registry writes, and
  service-account token or key minting, and emitted the cache grants as a required-grant
  contract rather than creating bindings the cache modules already own;
- moved idle shutdown into the guest so the instance needs no instance-admin authority over
  itself, and scheduled a daily stop with deliberately no start; and
- regenerated the module interface manifest to 46 modules and corrected `module_sources`,
  which listed 44 Bazel targets against 45 module directories.

No workstation instance, disk, identity, firewall rule, cache grant, or Nix substituter is
activated by this source change; the module also exports no NixOS, nix-darwin, or Home
Manager configuration. Mock tests prove configuration contracts and input rejection only.
Tunnel reachability, disk persistence across a real stop, idle-timer behaviour under a
detached build, Cloud NAT egress, and CMEK rotation remain unproven and are recorded as a
MISSING gate in `infra/terraform/PRODUCTION_READINESS.md`.

## 2026-08-22 — Fail-closed affected-input governance

- moved repository-wide affected-selection inputs into a strict versioned contract consumed by
  the selector and static architecture gate;
- added immutable minimum anchors, structural YAML/event-policy checks, and redacted error codes
  that reject weakened review boundaries or alternate protected-gate modes;
- expanded full-graph fallback to omitted Bazel, Nix, Python, Rust, launcher, and package-boundary
  inputs;
- pinned Git to the read-only Nix closure, require canonical non-worktree repository metadata,
  reject hidden index flags and host Git overrides, bind the disk cache to exact runner temporary
  storage, make the validated generated Bazel rc the sole cache-option authority, and validate
  canonical start epochs;
- parse workflow YAML with pinned PyYAML semantics, reject aliases, tags, and duplicate keys, and
  digest-pin each Bazel step in order so block-scalar, reordered, repinned, or spoofed verdict
  paths fail closed; and
- recorded the checksum-pinned target-determinator candidate plus the measured blockers that keep
  graph-native pull-request selection dormant.

Pull requests, merge groups, main pushes, and nightly runs retain full `//...`
execution. The Bazel-query selector remains source-qualified but inactive until
connected activation evidence exists. No graph-native selector,
authenticated remote cache, workflow gate, release, or deployment is activated
by this source change.
Artifact-plan Phase 5 remains incomplete pending the retained evidence above and activation of
the pinned organization required workflow outside the pull request's mutable trust boundary.

## 2026-08-22 — Evidence ledger and production eligibility

- added canonical, digest-sealed policy, claim, verification, deployment-bundle, and eligibility
  decision contracts with cross-language serialization vectors;
- added append-only PostgreSQL evidence, signed-decision, and revocation storage plus the isolated
  administrative API surface;
- added an HSM-backed Cloud KMS Ed25519 signer and fail-closed runtime configuration; and
- separated human evidence read/revoke permissions from the exact non-human submit/evaluate
  identity and rejected every other IAP service account.

This is source qualification only. The applied key, identity, database migration, IAP route, and
connected production workflow remain protected activation and operational-evidence requirements.

## 2026-08-22 — Activation-blocked Nix binary cache source

- corrected the reusable binary-cache module so a raw private GCS backend is never advertised
  as an authenticated Nix substituter, and exposed a machine-readable disabled client contract;
- added the exact x86_64 Linux CI-shell and package population inventory with protected-main,
  clean-SHA, no-client-signing-key enforcement; and
- added a zero-replica, no-egress, digest-pinned Attic source boundary that remains unusable until
  endpoint, authentication, HMAC, database, recovery, and cache-behavior evidence is reviewed.

No substituter, publisher, Kubernetes workload, secret value, or cloud resource is activated by
this source change. Attic remains upstream-labeled an early prototype and requires connected
review.

## 2026-08-21 — Common-document governance baseline

- replaced the one-line proprietary notice with the estate-wide, versioned
  proprietary software terms and a repository-specific third-party notice;
- standardized contribution authorization, security routing, support,
  conduct, and governance documents while preserving monorepo-specific
  architecture and qualification requirements;
- applied the canonical proprietary SPDX header to 320 tracked first-party
  source and policy files, while explicitly excluding independently licensed
  agent skills, generated clients, and machine-owned lock/reference files;
- made the complete root policy set part of the repository contract and
  validation path;
- added the exact estate-wide `LEGAL.md` reliance policy, upgraded the
  proprietary terms with the protected-disclosure notice, and made the
  license, conduct, and legal-policy texts fail-closed by digest;
- preserved the Contributor Covenant 2.1 attribution and modification boundary
  in the code of conduct and third-party notice;
- moved the reusable SPDX source-header template under `.github/` so `LICENSE`
  is the sole root license surface; and
- clarified that security response times are non-contractual operational
  targets and that safe harbor cannot authorize third-party systems or
  unlawful conduct.

## 2026-08-20 — Bazel graph production hardening

- retained the qualified Bazel 9.1.1/ruleset graph after testing the current LTS minor and
  finding that its lock-format migration could not be regenerated in the available environment;
- made every CI loading, layering, toolchain-selection, affected-test, and qualification
  invocation read the committed module lock without updating it;
- centralized noninteractive output under `--config=ci`, routed repository and documentation
  commands through `tools/dev/bazelw`, and added regression assertions for those contracts.
- added Bazel 8+ `REPO.bazel` traversal policy so nested Node, Python, and Terraform tool
  output cannot inject third-party packages, bytecode, or provider symlinks into `//...`.

This is local build-graph qualification, not remote-execution, connected-provider, artifact
publication, deployment, or production-promotion evidence.

## 2026-08-20 — Terraform v0.3.0 candidate

- added an explicit environment upgrade policy: development is the RAPID/CANARY cohort while
  staging, production, and control clusters remain REGULAR/QUALIFIED;
- added additive KMS encrypter/decrypter grants for live-owned service agents and allowed a
  dedicated server-access-log sink bucket to terminate recursive logging safely;
- removed the Binary Authorization module's project-wide
  `roles/containeranalysis.occurrences.editor` grant, which could update and delete release
  evidence, and exported the exact create/get/list occurrence contract for the authoritative
  `infrastructure-live` custom role;
- added the machine-checked 0.2.0-to-0.3.0 migration record for that intentional IAM address
  removal; v0.2.0 remained an unpublished planned contract and is not a production baseline.

This entry describes a release candidate, not a published tag or production deployment.
The live custom role, current GKE channel/version availability, connected plans, WIF/storage
canaries, empty drift plans, recovery, cost, and operational evidence remain promotion blockers.

## 2026-08-20 — Terraform v0.2.0 candidate (superseded before release)

- materialized and documented all 32 reusable Google Cloud modules plus the lifetime-scoped
  `dns_hub` root without creating deployable environment values or cloud state;
- added generated API documentation, an immutable v0.1.1 interface baseline, breaking-change
  classification, and a machine-checked 0.1.1-to-0.2.0 migration record;
- normalized reviewed Google provider locks to 7.45.0 and added backendless compatibility
  qualification at the declared minimum 7.41.0 and reviewed 7.45.0;
- added bounded Terraform CI, exact trusted provider caching, Trivy and Conftest gates, and a
  digest-bound saved-plan policy interface;
- SHA-pinned shared presubmit/security workflows and prepared `lint`/`terraform` required
  checks in an external ruleset that remains in evaluate mode pending real PR evidence.

This entry describes a release candidate, not a published tag or production deployment.
Live roots, connected plans, apply authorization, recovery, cost, and operational evidence
remain promotion blockers.


## 2026-08-19 — Estate audit remediation

Work on the `estate-audit-remediation` branch, which took the repository from a materialized
scaffold with no executing gates to one where the gates run. Grouped by what each change was
actually fixing rather than by date.

### Build and CI now execute

- gated the toolchains that had no gate, and built the first real image —
  `services/go_vanity`, chosen because it is small and genuinely real, to prove the
  build/sign/attest chain end to end;
- added a `bazel query //...` presubmit lane. It immediately found
  `services/workers/ingestion` declaring a source across a package boundary, a label Bazel
  refuses outright, which had been stopping `//...` from evaluating at all and therefore
  hiding every other Bazel result in the repository;
- declared the Go and Rust third-party dependency extensions in `MODULE.bazel`. Every Go
  BUILD file already carried `@org_golang_*`-style labels that resolved to nothing, so no Go
  target could be built — invisible because `go test` was doing the work and nothing asked
  Bazel;
- pinned `.bazelversion`, which did not exist, so bazelisk stopped choosing a Bazel per
  machine. It went to 8.4.2 first — rules_go 0.51 does not load under Bazel 9, `cross.bzl`
  using APIs it removed — and then to 9.1.1 once the rules_go 0.63.0 bump below removed that
  constraint. 9.1.1 is what `tools/build/nix/versions.nix` declares, and
  `checks/bazel-version.nix` now fails the build when the two disagree;
- added `.pre-commit-config.yaml`, the licence-header hook every other repository in the
  estate had carried since it was created and this one — the one with the most source in it —
  did not;
- made directly-invoked scripts executable, and let the `ci` Nix shell evaluate now that it
  carries Terraform (BUSL-licensed since 1.6, so the shell failed to *evaluate* without an
  `allowUnfree` predicate, before the shell existed, with an error naming no useful package).

### Infrastructure

- added the seven organization-layer security modules, including SCC and Access Transparency,
  and documented them;
- added a production-grade cleanup Makefile with dry-run and confirmation gates, covering
  Bazel, Python, Rust, Go, Node and local Nix/Terraform intermediates.

### Root files

- **materialized the seven reserved root paths that had never been written**: `.bazelrc`,
  `.bazelignore`, `.buildifier.json`, `.dockerignore`, `.editorconfig`, `.envrc`,
  `.gitattributes`. The repository had been running every Bazel invocation on stock defaults,
  which is why the presubmit query lane needed three flags on the command line to produce
  parseable output;
- **replaced the three root files that were materialized as stubs** — `OWNERS.toml`,
  `rustfmt.toml`, `bazel_downloader.cfg` — with real content. Blueprint coverage counts
  existence, not content, so all three had counted as done while saying nothing;
- **deleted five orphaned root snapshots**: `MANIFEST.sha256`, `REPOSITORY_TREE.txt`,
  `BLUEPRINT_COVERAGE.json`, `SCOPED_VALIDATION.json`, `go.mod.foundation-reference`. Nothing
  read them, no generator produced them, and all five described a tree that no longer existed
  — `MANIFEST.sha256` listed a `.bazelrc` the repository did not have. `BLUEPRINT_COVERAGE.json`
  was the source of the standing 100%-coverage claim, which had not been true since the Rust
  consolidation;
- reconciled `BUILD.bazel`'s root metadata exports, which had drifted to six of fifteen files
  and omitted `LICENSE`, `NOTICE` and `QUALIFICATION.md`;
- recounted the `VALIDATION.md` inventory against the tree and corrected the coverage figure
  to the measured 97.5%; corrected the stale `go.sum` line count in `QUALIFICATION.md`.

### Images are Bazel-built

- **`services/go_vanity` moved from a Dockerfile to `oci_image`**, which was the last failing
  architecture check — `check_build_toolchain_contract.py` forbids production Dockerfiles.
  The image is `go_library` → cross-compiled `go_binary` (`goos=linux`, `goarch=amd64`,
  `pure=on`) → `pkg_tar` → `oci_image` → `oci_push`, on the same distroless digest the
  Dockerfile pinned, so the migration changed the build system and not the base bytes;
- the resulting image is **bit-identical across a clean rebuild** — same manifest digest — a
  property the Dockerfile could not offer, because it ran `go mod download` inside the build
  and so depended on what the network served that day;
- `.github/workflows/release.yml` keeps calling `reusable-oci-build.yml` rather than growing
  its own copy of the signing chain. That workflow gained a `builder: bazel` mode — only its
  build step differs, since SBOM, cosign signature, SBOM attestation and Binary Authorization
  all key off `image@digest` — so the release job stays a `uses:` block and the estate keeps one
  signing chain. **Ordering constraint: this requires `reusable-oci-build.yml >= v1.2.0`**; an
  earlier tag ignores the `bazel-*` inputs as unknown and looks for a Dockerfile that is no
  longer there;
- the `platforms:` input is deliberately not passed under the bazel builder. It is buildx's;
  the target platform is now a property of the artifact (`goos`/`goarch` on the `go_binary`,
  and the amd64 base), and passing it again would be an unread second declaration free to
  drift.

### The Go SDK pin, which turned out not to be blocked

- **Bazel now compiles Go with 1.26.0, the version `go.mod` pins.** The standing note in
  `MODULE.bazel` said this was blocked because rules_go hardcodes a `GOEXPERIMENT` Go 1.26
  rejects, having verified 0.51 and 0.55 and concluded "every rules_go release in the
  registry". The registry serves up to 0.63.0, and the experiment is gone by then. Bumping
  rules_go 0.51 → 0.63.0 and pinning `go_sdk.download(version = "1.26.0")` builds clean across
  216 targets in `//libs/go`, `//control`, `//examples` and `//services/go_vanity`;
- that also closed `libs/go/httpx/middleware`, which used the Go 1.23 `net/http`
  `Request.Pattern` field and was the one target that built with `go build` and not with Bazel.

### Known open

- `MATERIALIZATION_BASELINE` was raised 46 → 114. Seven of that movement is this sweep closing
  root paths; the other 75 is two in-flight migrations (`training/distributed`,
  `libs/go/storage/outbox`) whose files moved without the blueprint manifest following. That
  is manifest reconciliation, owed by whichever change finishes those migrations.
- The release workflow remains fail-closed on the legacy `mindclade-org/.github` v1 contract.
  The current `mindclade/.github` v3 contract is intentionally not a drop-in replacement; a
  separate end-to-end build, qualification, and signing migration is required before release.
- Rust targets still fail Bazel analysis in the `crate_universe` extension, because `Cargo.lock`
  remains the unresolved connected-lane artifact `VALIDATION.md` describes. Unaffected by the
  rules_go bump.

## 2026-08-13 — Eighteen optimizations and canonical system design

### Added

- the user-supplied Rust library as the authoritative `libs/rust` starting
  implementation, followed by cohesive runtime/node deepening rather than a
  rewrite;
- signed runtime authority, route/revocation snapshots, execution/admission
  tickets, unified durable stages, artifact identity, reference releases, and
  release-evidence graph seams;
- deterministic resolved configuration, component maturity, dependency budgets,
  Go library admission, root-module, and Rust workspace enforcement;
- substantive preprocessing DAG/cache/provenance foundations and cross-language
  protocol fixtures;
- `docs/architecture/system-design-reference.md` as the canonical end-to-end
  system design covering control, runtime, data, preprocessing, models, training,
  serving, evaluation, artifacts, release, security, failures, scheduling, and
  qualification;
- `docs/architecture/system-design-traceability.md` mapping design decisions to
  source paths, ADRs, and evidence.

### Reconciliation and reproducibility

- made `docs/architecture/system-design-reference.md` the executable design contract
  through a code/docs alignment presubmit check;
- promoted only the implemented Rust runtime gateway/host cores to `implemented`
  maturity while retaining explicit production-qualification blockers;
- converted seven legacy Rust crate names into deprecated compatibility facades and
  removed active production dependencies on those legacy names;
- restored all 4,475 blueprint paths after the Rust consolidation using explicit
  non-authoritative scaffold markers where the detailed target tree no longer maps
  one-to-one to canonical crates;
- populated root `go.sum` with both checksum records for all 18 direct public Go
  requirements and added connected `download/verify/tidy` gates for transitive closure;
- replaced the repository-validation scaffold with executable hygiene, structured-file,
  Markdown-link, architecture, and optional Go qualification checks.

### Documentation

- refreshed repository and scaffold status language to distinguish implemented
  source from connected/provider/performance qualification;
- expanded MkDocs navigation through ADR-0023 and all post-scaffold architecture
  chapters;
- added architecture-level invariants, outage semantics, boundedness rules,
  service decomposition triggers, and end-to-end pipeline sequence descriptions.

## 2026-08-13 — Production scaffold and Go foundation

### Added

- complete target-state monorepo scaffold materialized from the production
  blueprint;
- fully implemented layered `libs/go` foundation;
- durable outbox, inbox, cursor, projector, work queue, leadership, migration,
  configuration, signing, pagination, and resource-version mechanisms;
- standardized `servicekit/production` composition and role capability checks;
- Go control-plane domain packages and fail-closed command roots;
- runnable control-plane API, event-dispatcher, and ingestion-coordinator
  examples using local adapters;
- architecture chapters, accepted ADRs, security docs, runbooks, Go usage
  cookbook, qualification records, and scaffold status documentation.

### Architecture

- Go owns fleet control and durable policy;
- Rust owns online/runtime data-plane and node execution;
- Python/PyTorch owns scientific and numerical semantics;
- TileLang kernels are enabled only through qualification manifests;
- Bazel owns the build/release graph and Nix owns pinned toolchains.

### Qualification note

Offline Go qualification is recorded in `VALIDATION.md` and `QUALIFICATION.md`.
Connected provider, Bazel, Nix, Rust, Python numerical, TileLang, and deployment
qualification remain explicit promotion gates rather than implied claims.
