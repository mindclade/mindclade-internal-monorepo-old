# GCP private services access baseline

This module reserves one internal IPv4 block on a custom-mode VPC and peers the
network with `servicenetworking.googleapis.com`. Managed services that attach
through private services access, such as Cloud SQL and Memorystore, carve their
service subnets out of the reserved block, so the range must never overlap
subnets, secondary ranges, or other reserved blocks in the same routing domain.

```hcl
module "private_service_access" {
  source = "../../modules/private-service-access"

  project_id          = "mindclade-production"
  network_id          = module.network.network_id
  reserved_range_name = "mindclade-production-psa"
  address             = "10.41.0.0"
  prefix_length       = 16
}
```

The reserved range and the peering connection carry Terraform lifecycle
protection, and the connection uses the ABANDON deletion policy because tearing
down an in-use peering strands every attached service instance. Custom-route
exchange with the producer network stays disabled unless a caller explicitly
enables it, for example so cross-region Cloud SQL replicas stay reachable over
hybrid connectivity.

Deleting or shrinking the range, moving it to another network, or removing the
peering while instances exist are outage-grade operations that require a
reviewed migration plan. The Service Networking API must be enabled on the
project, and the applying identity needs `servicenetworking.services.addPeering`
plus Compute address permissions. This module is a repository baseline, not
evidence that a live network is peered or qualified.
