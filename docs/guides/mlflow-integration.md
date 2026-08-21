# MLflow integration

Mindclade integrates with an upstream MLflow Tracking Server as an optional discovery and
comparison mirror. Mindclade's content-addressed artifact manifests, dataset snapshots, signed
execution tickets, model descriptors, and release evidence remain authoritative. MLflow run IDs,
aliases, and artifact paths are never accepted as substitutes for those digests.

The Python exporter uses the low-level `MlflowClient` shape and does not use MLflow's global active
run state. It serializes calls, bounds fields and lineage references, logs a canonical
`mindclade/lineage.json` containing digest references, and uses synchronous writes so a successful
call has an unambiguous meaning. The default mode is optional: an MLflow outage increments a local
failure counter but does not interrupt authoritative training or serving. Workflows that make the
mirror a release requirement must opt into `required=True` and include outage behavior in their
qualification evidence.

Production server requirements:

- run a supported upstream MLflow release behind TLS and an identity-aware proxy;
- configure explicit allowed hosts/CORS origins and never disable TLS verification;
- use a durable SQL metadata backend and object storage with retention, encryption, and backups;
- isolate workspaces/experiments and grant least privilege to workload identities;
- monitor API errors/latency, database saturation, artifact failures, queueing, and storage growth;
- test backup restore, schema upgrade, credential rotation, and rollback before promotion.

MLflow model serving and AI Gateway are not the Mindclade online serving authority. Models are
admitted by signed Mindclade descriptors and tickets and run through the Rust gateway/host plus
Python model worker. If MLflow deployment metadata is mirrored, it must point back to the immutable
Mindclade model/runtime digests and may not bypass the existing safety, rollout, or release gates.
