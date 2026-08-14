# Regional GKE platform module

This module creates a regional Standard GKE cluster and a dedicated redundant
system node pool. It requires an existing VPC/subnet and secondary ranges and
enforces private nodes and control plane with an RFC1918 `/28`, Dataplane V2,
Workload
Identity Federation for GKE, Shielded Nodes, Binary Authorization policy
enforcement, Google Groups for RBAC, intranode visibility, managed release/maintenance,
control-plane and workload logging,
Managed Service for Prometheus, cost allocation, deletion protection, and managed
CSI/backup agents. Restricted-data clusters require application-layer Kubernetes
Secrets encryption with Cloud KMS.

The current source qualification tuple pins the Regular channel at
`1.35.6-gke.1127000` for the control plane and initial system node version.
The Cloud Storage FUSE CSI driver defaults off: NOVA training uses
generation-bound object APIs and must not mount checkpoint storage through
GCS Fuse. Changing either value requires a new immutable platform lock and
upgrade, rollback, workload, and accelerator qualification evidence.

Dataplane V2 supplies Kubernetes NetworkPolicy enforcement itself. The module
deliberately omits the legacy `network_policy` API block because GKE rejects that
block when `ADVANCED_DATAPATH` is selected.

The monitoring contract includes managed DCGM metrics through Managed Service for
Prometheus. GPU qualification consumes those existing metrics and reads XID events
from the GKE GPU device-plugin logs; it does not deploy a second DCGM exporter.

The module does not create workload node pools, Kubernetes namespaces/RBAC/policy,
Backup for GKE plans, Binary Authorization attestations, firewall rules, NAT, DNS,
KMS IAM, service-account IAM, notification channels, or quotas. Those cross state
and privilege boundaries. The system-node service account must be user managed and
keyless; Pod access uses Kubernetes service accounts through Workload Identity.

The pin and Terraform plan tests are **Proposed deployment configuration**, not
observed cluster state. Before rollout, verify current GKE version/region support, IP capacity, private
control-plane reachability, maintenance exclusions, release compatibility, quotas,
Pod Security/NetworkPolicy admission, Binary Authorization deny canaries, log and
metric routing, alert paging, backup/restore, upgrade/rollback, zonal impairment,
and cost attribution. Offline tests do not create or exercise a cluster.
