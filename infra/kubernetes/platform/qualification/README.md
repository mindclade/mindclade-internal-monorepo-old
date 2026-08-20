# GKE foundation qualification package

This environment-neutral package defines a dedicated `mindclade-qualification` namespace and
one suspended CPU, H100, and H200 Job. It is infrastructure qualification, not a product workload
or a capacity queue. Normal CPU/training queues remain held and their quotas remain zero.

The base is deliberately impossible to run: the namespace is activation-blocked, Pod and Job
quota is zero, every Job is suspended, and every container uses the zero digest under
`registry.invalid`. The H100 and H200 profiles request all eight GPUs on one qualified node so a
connected run can exercise full-node CUDA/NCCL behavior without claiming cross-node fabric
coverage.

Only `mindclade/gitops` may compose an environment activation overlay. One overlay selects exactly
one environment, cluster, and hardware profile and must atomically provide a signed qualification
request, a real attested image digest, measured exact quota, an environment-bound node selector,
and an unsuspended uniquely named Job. During that run the namespace activation value is exactly
`qualification`, never `active`. Cleanup must delete the run, restore zero quota, and return this
namespace to `workload-activation=blocked`. It must not unhold or modify a normal workload queue.

The source image initially contains the CPU/local-storage/UDS probe. GPU runs remain fail-closed
until the release image also contains a pinned CUDA/NCCL helper and its provenance; seeing a GPU
through `nvidia-smi` is not accepted as CUDA/NCCL qualification. The helper contract requires all
eight CUDA devices, at least a 1 GiB NCCL all-reduce, a positive measured bus bandwidth, at least
1 GiB of tested GPU memory, and a passing DCGM health result.

