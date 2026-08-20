# RDMA transport boundary

No RDMA device resource, host device, attachment, or privilege is guessed here. The Terraform
profile indicates H200 GPUDirect-RDMA capability, but the Kubernetes device and isolation
contract must be observed from a provisioned staging cluster. Selected RDMA pods are denied
ordinary network traffic in the meantime, and all GPU/queue quotas remain zero.

Activation requires device-plugin inventory, VPC/firewall and tenant isolation review,
non-privileged access where supported, multi-node correctness/throughput, failure and drain
tests, and proof that Kubernetes policy covers every relevant path. Rollback suspends JobSets,
holds Kueue, removes device allocation from templates, and only then drains nodes.
