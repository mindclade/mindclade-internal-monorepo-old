# ARC self-hosted runners runbook

## Status and authority

This runbook operates the Actions Runner Controller plane that serves this repository's CI. The
machine-readable authority is `arc/presubmit-readiness.yaml` in the `gitops` repository, and the
activation procedure lives beside it in `docs/arc-ci-activation.md`. This page explains how to work
the plane and why each constraint holds. Where the two disagree the readiness contract wins — it is
what the validator reads.

No single repository owns this plane, and reaching for the wrong one is the usual reason an
activation ends up half-applied:

| Concern | Authority |
|---|---|
| Nix base and the future Actions runner image | `mindclade-internal-monorepo` |
| Cache, cluster, node pool, workload identity, secret prerequisites | `infrastructure-live` |
| Runner group membership and permitted workflow routing | `github-config` |
| Reusable workflow behaviour | `.github` |
| ARC values, namespace policy, Argo selection | `gitops` |

Presubmit capacity is **not** active. Nothing below describes observed behaviour of a running
presubmit runner; treat every claim about that lane as design intent until the connected evidence
named in the readiness contract is attached.

## Two runner groups, and why they must stay two

There are two groups, and the boundary between them is the entire security design:

- **`mindclade-arc-artifact-authority`** backs the `mindclade-arc-canary` (max 1),
  `mindclade-arc-build-cpu` (max 6), and `mindclade-arc-qualify-cpu` (max 4) scale sets. These serve
  trusted post-review work and are the lane that may eventually hold signing and publication
  authority.
- **`mindclade-arc-ci`** backs `mindclade-arc-presubmit-cpu` and nothing else. It runs pull-request
  code — code no reviewer has read, submitted by anyone who can open a pull request.

Presubmit must never hold signing or publication authority. Stated so it is checkable: *no workflow
reachable from `mindclade-arc-ci` may sign, attest, or publish an artifact.* A single merged group
would mean a fork's pull request could land on a runner whose identity can push to the registry, and
no amount of workflow-level care recovers from that — the credential is reachable from the process,
not from the workflow file.

The same boundary is held from the other side by the developer workstation, whose module refuses
signing, publication, and attestation roles by name; see [`workstation.md`](workstation.md). Two
independent enforcement points for one property is deliberate. Do not "simplify" either of them.

## Current state: presubmit is blocked, and blocked means inert

`arc/presubmit-readiness.yaml` carries `phase: blocked`, `selected: false`, `minRunners: 0`, and
`maxRunners: 0`. The activated target is `minRunners: 2` / `maxRunners: 24`.

The zeros are not the only thing stopping activation, and that is the part worth internalising
before reviewing any change here. `arc/rendered/presubmit.yaml` is absent from
`arc/rendered/kustomization.yaml`, no `arc-presubmit` namespace exists, no Secret Sync resource
targets it, and no Argo root can reach it. Merging the dormant source therefore cannot register a
runner or start a pod — it is source that no reconciler reads. That layering exists so a review
mistake in the values file is not, by itself, an activation.

The activated ceiling of 24 is deliberately higher than the release lane's 6. Presubmit fans out
across the full unconfigured graph on merge-group runs while the release lane serialises. The floor
of 2 absorbs queue latency: a scale set at zero pays a pod-start and runner-registration round trip
on the first job of every quiet period. Neither number moves without cluster quota, cache-service,
queue-latency, and cost evidence behind it.

## The six readiness gates

Six gates guard activation: `nixBinaryCache`, `runnerImage`, `runnerGroup`, `readOnlyCacheWif`,
`connectedCluster`, and `workflowRouting`. Each requires an immutable evidence object recorded in
the readiness contract with its **URI, SHA-256, reviewer, and UTC timestamp**.

The rule that gets broken is the simple one: **do not mark a gate qualified from source
inspection.** Reading `arc/values/presubmit.yaml` and confirming it says
`automountServiceAccountToken: false` tells you what the chart intends to render. A gate is a claim
about a connected system — that the token was in fact absent from a pod that in fact ran. Source is
the input to the test, never its result.

1. **`nixBinaryCache`** — a Linux `x86_64` runner substitutes every required CI shell from the
   protected cache, with no source build, and with cache signatures verified. A runner that compiles
   its own toolchain closure per job is not a latency optimisation; it is a slower hosted runner
   with more attack surface.
2. **`runnerImage`** — see the next section. Recorded by exact repository, digest, source commit,
   flake package, and release record.
3. **`runnerGroup`** — `mindclade-arc-ci` exists separately from artifact authority and permits only
   reviewed presubmit callers. Confirm against the **live GitHub API** whether authorization is
   evaluated against the calling workflow. Do not infer this from catalog source: the catalog records
   what was requested, the API records what GitHub will enforce.
4. **`readOnlyCacheWif`** — the runner identity can read the Bazel and Nix caches and cannot write
   them, push packages or images, create attestations, use signing keys, or impersonate a release
   identity. Prove each of those negatively; a read that succeeds is not evidence that a write fails.
5. **`connectedCluster`** — target cluster, ARC controller, workload identity, network policy, Secret
   Sync CRDs, and bounded resource capacity all pass connected qualification.
6. **`workflowRouting`** — an internal test repository proves the exact scale-set label accepts the
   permitted workflow and **rejects** an unlisted workflow, a fork, and an artifact-authority job.
   Three negative results, not one positive one.

## The runner image, and the fixture that must never become it

Every values file in `arc/values/` currently names
`ghcr.io/actions/actions-runner:2.336.0@sha256:0cfd…cdda`, and `presubmit-readiness.yaml` records
that same reference under `runnerImage.validationFixture`. **It is a render fixture.** Its only
purpose is to let the Helm chart render offline so the generated objects can be reviewed, and the
readiness contract holds it in a field that can never satisfy the `runnerImage` gate.

The real image must be a **monorepo Nix package extending `.#remote-execution-base`**, containing
the Actions runner and the required CI shell closures, published by the protected release lane. The
fixture cannot become that image by being promoted, re-tagged, or copied into
`runnerImage.repository`. It carries none of this repository's pinned toolchain — no
`tools/build/nix/versions.nix` closure, no agreement with `toolchain-manifest.json` — so a presubmit
job running on it would execute a toolchain no gate here covers, while the `runnerImage` gate
reported qualified against an artifact nobody in this organisation built.

`remote-execution-base` is gated to Linux, so an `aarch64-darwin` laptop cannot build it. The
`x86_64-linux` builder is the developer workstation; that machine can reproduce the digest and
explicitly cannot sign, publish, or attest it. Reproducing a digest is evidence about a build, not a
release decision.

When the real image exists, point `arc/values/presubmit.yaml` at exactly `repository@digest` and
make the renderer accept that image only when it matches the qualified readiness contract. Never
hand-fill a digest.

## Ephemerality is the containment boundary

A presubmit runner is the one place in this estate where untrusted code executes on infrastructure
we own. Containment rests on properties that are all declared in the values file today and none of
which have connected evidence yet:

- `automountServiceAccountToken: false`. The chart-created service account has no permissions, and
  the pod does not receive its token anyway. A projected token inside a pod running a fork's build
  script is a Kubernetes API client handed to an anonymous contributor.
- Run-once pods with `restartPolicy: Never`. The pod is created for one job and deleted after it. A
  pod that restarts is a pod that reuses a workspace, and workspace reuse across jobs is how one
  pull request reads or plants files for the next.
- No shared writable cache volume. This is the cross-job write primitive the design refuses. A
  shared cache between untrusted jobs lets pull request #1 poison an artifact that pull request #2
  consumes, with neither author aware of the other. Cache *reads* are the point of
  `readOnlyCacheWif`; cache *writes* from this lane are exactly what is being denied.
- `terminationGracePeriodSeconds: 120` bounds how long a job's process may take to exit before the
  pod is removed regardless.

**The GitHub App secret is a controller credential, never a runner credential.** `arc-github-app`
lives in the `arc-systems` namespace and is consumed by the ARC controller to register scale sets.
It is not mounted into any runner pod, and it must not be, in either group. The controller exchanges
it for a short-lived per-runner registration token, and that token is what a runner holds. If the App
secret is ever reachable from a runner pod, that is a stop condition on the spot: it is an
organisation-wide credential, and the group split means nothing once presubmit can read it.

## Node placement: the pool exists, the toleration does not

`infrastructure-live` now applies a dedicated runner node pool at `5-workloads/ci/nodepools/runner`,
named `arc-runner`, carrying:

- taint `scheduling.mindclade.dev/arc-runner=true:NO_SCHEDULE`;
- node label `mindclade.dev/workload-class=arc-runner`; and
- a node service account `sa-arc-runner-nodes`, distinct from the controller pool's
  `sa-arc-system-nodes`.

**Runners are not on it yet.** The scale-set values in `gitops/arc/values/*.yaml` carry a
`nodeSelector` of `iam.gke.io/gke-metadata-server-enabled: "true"` and no `tolerations` entry at all.
Every node pool in the cluster satisfies that selector, and no pod without a matching toleration can
land on a tainted node — so today every runner, in both groups, still schedules onto the **system
pool, beside the ARC controller**.

That is the defect the pool was created to close. The controller decides which jobs run and holds
the App secret; runner pods execute untrusted pull-request code. Co-tenancy means a container escape,
or plain resource exhaustion on a runner, takes out the controller and every other runner with it,
and the blast radius becomes the whole artifact-authority cluster instead of one job. The presubmit
resource limits — 12 CPU and 24 GiB — make the noisy-neighbour half of that concrete without any
escape at all.

Two things about ordering, because getting them wrong is how this becomes an outage rather than a
no-op:

- The taint fails **closed**. Applying the pool without the toleration moves nothing, which is why
  `infrastructure-live` could go first. A toleration is likewise safe to add before its pool exists,
  because it tolerates a taint nothing carries.
- The dangerous half-step is a toleration **without** a matching `nodeSelector` on
  `mindclade.dev/workload-class=arc-runner`. A toleration grants permission to land on the tainted
  pool; it does not require it. A runner that merely tolerates the taint remains free to schedule
  back onto the system pool, and the isolation you believe you applied is scheduler luck. Add both,
  in the same change, or add neither.

Re-derive the pool's ceiling before presubmit scales rather than assuming it covers the new lane.
`total_max_nodes` is 6, and the sizing reasoning behind it counts the artifact-authority group only —
build 6 plus qualify 4 plus canary 1, at roughly three 2-vCPU runners to an `n2-standard-8` once the
GKE daemonsets have taken their share. The activated presubmit target of 24 runners is additional
demand of the same shape, and six nodes do not cover both. The node-pool ceiling is an input the
presubmit scaling change must revisit in `infrastructure-live`; a `maxRunners` bump cannot raise it.

The pool is `ON_DEMAND` with `spot_approval` null, deliberately. Spot looks like free money on a pool
that idles at zero, and it is wrong here: a preempted runner does not reschedule its job, it fails
it, and in the GitHub UI a preemption-failed presubmit is indistinguishable from a real test failure.
Revisit only against measured eviction rates for that machine shape and region.

## Activation is three reviewed changes, never one

**One — qualified source.** Fill the runner-image fields from protected release evidence, attach an
evidence object to each of the six gates, set `phase: qualified`, and keep `selected: false` with
both capacity values at zero. Validate without cluster access, then merge. A qualified source still
creates no namespace, no registration, and no pods.

**Two — protected canary.** Only after all six gates remain qualified. Apply the `mindclade-arc-ci`
group and its exact workflow allowlist in `github-config`, and the read-only cache identity and
cluster prerequisites in `infrastructure-live`, through their protected plan/apply paths; record the
applied outputs and never copy raw credentials into Git. Add the `arc-presubmit` namespace with
restricted Pod Security labels, default-deny plus required egress policies, and controller-only
Secret Sync. Add the AppProject, Argo configuration, root, and Application at the exact paths the
validator enforces, and add `presubmit.yaml` to `arc/rendered/kustomization.yaml` — in this change
and no earlier. Set `phase: canary`, `selected: true`, `minRunners: 0`, `maxRunners: 1`.

Then run one non-publishing job from an approved operator session and prove, individually: the pod
terminates after the job, no persistent volume exists, cache access is read-only, forbidden workflow
routing is denied, and no signing or push credential is reachable. Store the connected record at the
restricted evidence boundary with commit, image digest, workflow run ID, pod UID, timestamps,
reviewer, and SHA-256.

Any missing object, a job queued on the wrong scale set, an unexpected credential, a mutable image, a
successful cache write, a surviving pod, or shared storage is a **stop condition**. Revert the
activation change. Do not weaken the validator or the runner-group restriction to get past it — that
is precisely the failure this structure exists to make impossible.

**Three — scale.** After the canary evidence is independently reviewed, a third pull request sets
`phase: activated`, `minRunners: 2`, `maxRunners: 24`. Observe at least one pull request and one
merge-group full-graph run, and confirm cache-hit rate, startup latency, CPU and memory saturation,
pod deletion, rejected cache writes, and hosted-runner fallback before making the ARC label a
required merge context. Making it required earlier means a runner-plane fault blocks every merge in
the repository with no fallback.

## arc-controller-target-absent

The controller's telemetry target is gone, so runner-plane health cannot be established. The
threshold is structural rather than a reviewed objective: the series is a target-up gauge, so
anything below one means no evidence exists. This is the one signal here with `missingData: fire`,
because absent data *is* the condition being detected.

Establish whether the controller is down or merely unscraped before touching it. Those have opposite
responses, and restarting a healthy controller to fix a broken scrape path destroys the evidence.
Check the controller Deployment in `arc-systems` — two replicas spread across zones by a
`DoNotSchedule` topology constraint, so a single-zone event should degrade it rather than remove it —
and then the `PodMonitoring` resource and its target status.

While the controller is absent, in-flight runner pods keep running their jobs and no new runner is
registered or scaled. Queued jobs wait; they do not fail over to hosted runners on their own.

## arc-controller-reconcile-error-ratio

Reconciliation errors exceed the proposed ratio over a sufficient controller event sample. The
threshold is `proposed`, not approved — a starting point, not an objective anyone has committed to.

This is worth a ticket because a controller that cannot converge leaves ephemeral runners **orphaned
rather than failing visibly**. The scale set reports a desired count, pods exist that no longer
correspond to a job, and nothing surfaces as a red check anywhere a developer looks. Read the
controller log for the first distinct error rather than the most frequent one: repeated registration
failures against the GitHub API and repeated pod-creation failures against the scheduler look alike
in aggregate and have nothing in common.

If reconciliation is failing against the API, confirm the App installation and the registration
credential's validity before assuming a cluster fault. If it is failing at pod creation, the node
pool, quota, or Pod Security admission is the likelier cause — see the placement section, because a
runner that cannot schedule at all and a runner scheduling onto the wrong pool are two outcomes of
the same misconfiguration.

## arc-runner-pool-saturation

Ephemeral runners in a bounded scale set hold above the proposed share of their configured maximum.
The ratio is measured **per runner group**, and that is not a detail: a saturated build group and
pressure on the separately governed release group are different events with different responses, and
collapsing them into one number pages the wrong people about the wrong lane.

Sustained saturation on a bounded pool is often the pool working as designed rather than a fault.
Confirm in this order:

1. Is the queue draining? Compare against the observed `arc-job-queue-wait-p95` baseline. A pool at
   its ceiling with flat queue wait is correctly sized and busy.
2. Is `arc-runner-pool-maximum` the number you think it is? The saturation ratio is divided by that
   bound, and an unobserved or drifted maximum makes the ratio meaningless.
3. Is the node pool, rather than the scale set, the actual bound? A scale set at `maxRunners: 6`
   whose pods sit `Pending` is not saturated, it is unschedulable, and raising `maxRunners` makes it
   worse.

Raising a ceiling is a reviewed change with cluster quota, cache-service, queue-latency, and cost
evidence behind it. Raising it to clear a backlog mid-incident is the reflex to resist.

## Rolling back

Order matters, and it is the reverse of what feels urgent:

1. **Route workflows back to `ubuntu-24.04` first.** Zeroing capacity while jobs still target the ARC
   label leaves them queued against a scale set that will never have a runner — an outage that
   presents as a hang.
2. Set `minRunners: 0` and `maxRunners: 0`, remove `presubmit.yaml` from
   `arc/rendered/kustomization.yaml`, and merge the reviewed GitOps rollback.
3. Preserve the runner image, logs, and evidence. Rollback is not cleanup.
4. Revoke the read-only runner identity or registration access only through its owning repository,
   and only after no job is running.

Never delete shared release-lane ARC resources as part of a presubmit rollback. The controller, the
App secret, the `arc-systems` namespace, and the artifact-authority scale sets are shared
infrastructure; removing them to roll back presubmit takes the release lane down with it.

## Troubleshooting

| Symptom | Cause | Action |
|---|---|---|
| a job queues forever against the ARC label | the scale set is `selected: false` or at `maxRunners: 0` | expected while presubmit is blocked; route the workflow back to a hosted runner rather than raising capacity |
| runner pods sit `Pending` with no scheduling target | the node pool is at `total_max_nodes`, or a pod tolerates the runner taint without selecting the pool | read pod events before the scale set; see the placement section |
| runner pods land on the system pool | no `tolerations` and no `workload-class` selector in `arc/values/*.yaml` | this is the current state, not a regression; the fix needs both fields in one change |
| a job fails immediately with a registration error | the controller cannot exchange the App credential | inspect the controller, not the runner; a runner never holds the App secret |
| a presubmit job succeeds at pushing or signing something | the group split or the WIF binding has failed | stop condition — revert activation, preserve evidence, and do not re-enable to reproduce |
| a job fails partway through with a workspace or disk message | node boot-disk pressure; checkout, Bazel outputs, and image layers share the node's writable layer | inspect node disk before re-running; the pool provisions 200 GiB for this reason |
| a presubmit failure that looks like a flaky test | a preempted or drained node failed the job rather than rescheduling it | check node lifecycle events; this is why the pool is `ON_DEMAND` and the drain window is long |
| the render fixture image appears in a qualified contract | the validation fixture was promoted into `runnerImage` | reject the change; the fixture can never satisfy that gate |

Preserve pod events, the controller log, the scale-set status, and the workflow run ID before
deleting anything. A deleted runner pod takes every containment observation with it, and the pod is
the only place several of them can be made.

## Exit criteria

The plane is healthy when the controller reconciles without sustained error, each scale set's runner
count tracks its queue, pods are created and deleted per job with no survivors, and both groups
remain distinct with no workflow reachable from `mindclade-arc-ci` capable of signing or publishing.

Presubmit is qualified only when all six gates carry immutable connected evidence; the runner image
is a monorepo Nix package extending `.#remote-execution-base`, recorded by digest and release record;
a canary job has demonstrated pod termination, read-only cache access, denied forbidden routing, and
no reachable signing or push credential; and runner pods schedule onto the dedicated `arc-runner`
pool rather than beside the controller.
