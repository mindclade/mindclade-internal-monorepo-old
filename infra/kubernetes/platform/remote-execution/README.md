# Remote execution boundary

This root materializes the activation-blocked Buildfarm 2.17.0 server and worker contract on a
private, restricted `mindclade-build` namespace. Both controllers intentionally render with
zero replicas. Images use the multi-platform index digests locked in
`infra/build/remote_execution/images.lock.json`; the worker pool selector and keyless service
account agree with the Terraform `bazel_remote_execution` handoff.

An operator overlay must supply an internal HA Redis endpoint, organization-mirrored image
references, the workload-identity annotation, TLS termination/mTLS policy, and the attested Nix
AMD64/ARM64 execution-base digests before changing replicas. The default-deny policy has no
external Redis or client allowance, so editing replicas alone cannot create a working or exposed
service.

Activation requires local/remote digest parity, evidence of executed (not cache-only) actions,
cache cold/warm/corruption/eviction tests, bounded cancellation and drain, multi-zone failover,
private connectivity, cost/capacity limits, and SLO evidence. Rollback removes every Bazel
client endpoint before scaling workers down; cache deletion remains a separate reviewed
data-lifecycle action.
