# GPU scheduling contract

This module mirrors the exact node labels and taint owned by Terraform's `gpu_node_pool` module
and adds a zero GPU quota in `mindclade-system`. It does not install drivers, advertise devices,
or guess a RuntimeClass handler; those are observed cluster-provider contracts.

Activation requires node/device-plugin health, H100 and H200 smoke tests, quota and autoscaling
bounds, topology and GPUDirect qualification, runtime-host IPC readiness, numerical parity,
checkpoint/restore, cancellation, and cost attribution. Raise both Kubernetes and Kueue GPU
quotas together only after that evidence exists. Rollback sets both quotas to zero, holds the
queue, and drains only checkpoint-safe work before changing node pools.
