# Release evidence

No model, runtime, service, dataset, checkpoint format, kernel provider, or
reference database is promoted solely from a successful build.

## Evidence bundle

A release candidate links immutable evidence for:

- source commit and clean/approved change state;
- resolved configuration and dependency/toolchain manifests;
- Bazel invocation, action graph, remote execution platform, and build outputs;
- OCI image digests, SBOMs, vulnerability/license scans, signatures, provenance;
- model/runtime/checkpoint/dataset/reference manifests;
- numerical parity, determinism, gradient, checkpoint-resume, and cross-language tests;
- performance, capacity, latency, throughput, memory, and scale results;
- capability, robustness, privacy, biological-risk, safety, and misuse evaluations;
- access-control, tenant-isolation, audit, and incident-response qualification;
- rollout, rollback, migration, and compatibility rehearsal.

## Promotion

```text
candidate -> qualified -> canary -> staged -> production
                          \-> rejected
```

Each transition is resource-version guarded, authorized, audited, and appended
to the transactional outbox. Route snapshots contain only approved immutable
bundle digests.

## Rollback

Every production promotion has a last-known-good target, compatibility statement,
rollback command/target, database/config migration constraints, and rehearsed
recovery evidence. Emergency revocation advances the policy epoch so Rust
runtime gateways stop admitting new work even while the control plane is
partially unavailable.

## Retention

Release evidence is content-addressed, signed, access-controlled, and retained
at least as long as the promoted asset and required regulatory/audit period.
