# Internal address module

This module reserves protected regional internal IPv4 addresses from existing
private subnetworks. Address lifecycle is separate from the VPC because GKE Gateway
and private DNS consume the stable address name/value after network creation.

```hcl
module "internal_addresses" {
  source = "../../modules/internal_address"

  addresses = {
    production-gateway = {
      project_id = "mindclade-production"
      name       = "production-gateway"
      region     = "us-central1"
      subnetwork = "projects/mindclade-network/regions/us-central1/subnetworks/production-nodes"
      address    = "10.20.0.10"
    }
  }
}
```

Resources use both provider deletion policy and Terraform lifecycle protection.
Changing an address after DNS or a Gateway references it is an outage-grade migration
requiring coordinated changes and verification. The caller owns subnet existence,
IPAM/non-overlap, DNS records, firewall policy, Gateway configuration, and proof that
the optional literal address is free and inside the subnet. This module does not
allocate public, global, proxy-only, or Private Service Connect addresses.

