# Canonical workload envelope

One workload envelope represents durable ingestion, curation, preprocessing, reference building, batch inference, evaluation, training, checkpoint/artifact transfer, rollouts, and simulation. It binds workload/run/job/stage/tenant/workspace identity to a signed execution ticket, immutable input/output identities, resolved configuration digest, resource class, creation time, and deadline.

Go owns workload lifecycle and attempt/fencing policy. Rust validates and executes bounded stages. Python receives only the process-local scientific/numerical projection after Rust authority checks have succeeded.
