# H100 training qualification

This package owns the source-side, fail-closed runner for the bounded reference
training platform. It supports exactly two ordered on-demand H100 phases:

1. one-GPU smoke (`h100-1g-smoke`);
2. one-node, eight-GPU DDP/DCP (`h100-8g-ddp-dcp`).

The checked-in Job and JobSet are suspended and use all-zero image digests under
`registry.invalid`. The runner cannot activate capacity, modify queue policy,
select a GitOps release, or publish evidence. Those remain reviewed operator and
release-system responsibilities.

Connected submission is also deliberately disabled in this source revision. A
trainer-emitted JSON line is only an observation: it cannot independently prove
its own image, hardware, numerical tolerance, checkpoint bytes, resume parity, or
serving parity. Activation requires an external collector/verifier that derives
those assertions from the observed Pod UID and image IDs, scheduled node identity,
immutable CAS bytes, approved tolerances, and authorized signer attestations. The
CLI fails before preflight or submission until that authority is wired.

A live invocation requires an absolute, non-symlink cohort JSON document matching
`cohort.schema.json`. The cohort binds the source revision, resolved config,
dataset, model contract, toolchain, both images, checkpoint schema, zone, exact
on-demand node profile, pricing snapshot, and both ordered phases. The runner
checks live capacity only for the selected phase, submits exactly one object, and
writes one new immutable local evidence file. Existing evidence is never
overwritten.

The safe source-only entrypoint validates the held templates:

```bash
tools/dev/nixw develop .#ci --command tools/dev/bazelw run \
  //tools/qualification/training_gke:run -- --validate-only
```

The one-GPU smoke is a prerequisite and is not part of production SLO statistics.
The eight-GPU runner requires the append-only one-GPU evidence file through
`--prerequisite-evidence`; it verifies that file's digest, success invariants,
images, and cohort before submitting the JobSet.
Production qualification additionally requires at least 30 independent terminal
eight-GPU target-profile staging runs, owner-approved SLO/RPO/RTO/cost thresholds,
failure-injection evidence, alert fire/resolve evidence, security evidence, and a
rollback drill. This package records observations; it does not approve thresholds
or make a production claim.

After connected characterization, `//tools/qualification/training_gke:verify_qualification`
performs structural validation of the closed qualification-set projection. It counts runs rather than attempts,
excludes only pre-admission rejection and explicit user cancellation, requires at
least the owner-approved minimum of 30 eligible eight-GPU runs, compares the
observed completion ratio with the exact parts-per-million objective, and requires
distinct evidence for every counted run, the exact operational evidence, and three
independent approval artifacts. The
policy digest is derived from the canonical integer-only policy projection; every
approval is bound to that policy, the release subject, and the external threshold
artifact. Digest-shaped strings and caller-provided booleans are not proof. The Go
release service must additionally resolve every typed artifact, recompute its
digest, validate signer authority/profile/freshness, and match the active policy
digest and epoch before promotion; its production factory is intentionally inert
until that resolver and approved policy authority are configured. Unit-test
fixtures exercise structural logic only and are not shipped qualification evidence.
