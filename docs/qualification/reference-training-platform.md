# Reference training platform qualification

## Claim boundary

The production candidate is the bounded training platform, not the affine
workload as a scientific model. `reference-affine-v1` is a deterministic
conformance workload used to prove model, optimizer, checkpoint, bundle, worker,
and serving seams. The `models` and `training` umbrellas remain scaffolded.

The initial support tuple is deliberately closed:

- eager float32;
- CPU world sizes 1 and 2 with Gloo;
- on-demand NVIDIA H100 world sizes 1 and 8 with NCCL;
- single-node execution at world size 8;
- eager safetensors model bundles;
- pickle-free DCP with same-world exact resume and explicitly replicated 1-to-2
  and 2-to-1 portability.

B200/H200, Spot capacity, multi-node training, FSDP, arbitrary resharding, mixed
precision, ONNX/AOTInductor, custom CUDA extensions, TileLang, and general
scientific models are outside this claim.

## Ordered connected sequence

All capacity remains held with zero quota until a reviewed operator change. The
checked-in runner currently validates source templates only and refuses connected
submission until an independent evidence collector/verifier is composed. Once that
authority exists, a connected qualification will use one immutable cohort document
accepted by `//tools/qualification/training_gke:run`. The cohort binds source, configuration,
dataset, model contract, toolchain, both images, checkpoint schema, zone,
on-demand node profile, pricing snapshot, and ordered phase list.

1. Run one H100 GPU smoke. It must prove forward/backward/optimizer execution,
   checkpoint save/restore/continuation, cancellation, serving parity, and
   optional-MLflow outage isolation. It is a prerequisite and is excluded from
   SLO statistics.
2. Run one single-node eight-H100 NCCL/DCP qualification. It must prove exact
   rank/device mapping, disjoint and complete data sharding, global-denominator
   loss reduction, checkpoint/resume, and eager-serving parity.
3. Characterize at least 30 independent terminal runs of that exact eight-GPU
   profile. The denominator is admitted runs that start execution. Recovered
   attempts count once; platform failures and deadlines are bad. Pre-admission
   rejection and explicit user cancellation are excluded and counted
   separately.
4. Training Platform, SRE, and FinOps review measured distributions and approve
   completion SLO, RPO/RTO, checkpoint pause/throughput, stalls, performance,
   capacity headroom, and unit-cost bounds. Proposed alert values are not
   approvals.
5. Run the documented failure matrix and non-production alert fire/resolve
   drills, then rehearse rollback. Only immutable evidence produced from those
   connected runs may enter the release graph.

## Promotion evidence

`control/registry/releases.ProductionPolicy` fails closed unless its active policy
digest and epoch are configured, every required kind occurs exactly once, and every
node carries an immutable artifact bound to the graph subject and policy. Promotion
also requires an injected verifier to resolve the typed artifact bytes, recompute
their digests, and validate signer authority, profile, freshness, and derived result;
caller-supplied `kind` and `passed` values are not proof. The production factory is
intentionally unconfigured until those connected authorities are composed. In
addition to source/config/data/training/model/runtime and supply
chain evidence, production now requires checkpoint-resume, numerical,
scale/performance, reliability/failure-injection, SLO approval, alert
fire/resolve, cost, security/vulnerability, lineage, rollback-drill, and exact
H100 one-GPU and eight-GPU qualification nodes.

The bounded evidence operation accepts only an existing `candidate` release in
`qualified` state with a positive resource version. PostgreSQL compare-and-swaps
the exact durable subject, evidence graph, channel, state, and version before
changing only the state to `promoted`; it has no insert path, and the graph and
state update share one serializable transaction. `promoted` is an evidence-review
state, not a routable production channel. Audit/outbox publication and the
authorized canary-to-staged-to-production state machine remain deliberately
uncomposed activation gates.

CAS/GCS and the checkpoint registry remain authoritative. MLflow is an optional
mirror: its run ID or alias is never identity or promotion authority, and an
optional-mode outage cannot fail authoritative training. No source-only or local
test result satisfies a connected evidence requirement.
