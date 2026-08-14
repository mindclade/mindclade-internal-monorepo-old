# GCP network baseline

This module creates one custom-mode IPv4 VPC, protected private subnets, an
explicit default-internet-gateway route, and optional subnet-scoped Public Cloud
NAT gateways. Private Google Access is mandatory and VPC Flow Logs are enabled by default.
Cloud NAT always logs and can use Google-managed or caller-provided addresses.
Manual addresses must be canonical regional Compute Address self-links in this
project, and router/NAT identities are unique within a region.

```hcl
module "network" {
  source = "../../modules/network"

  project_id   = "mindclade-network-prod"
  network_name = "mindclade-prod"
  subnets = {
    prod-central = {
      region        = "us-central1"
      ip_cidr_range = "10.20.0.0/20"
      secondary_ip_ranges = {
        prod-pods     = "10.24.0.0/14"
        prod-services = "10.28.0.0/20"
      }
    }
  }
  nat_gateways = {
    central = {
      region      = "us-central1"
      router_name = "mindclade-prod-central-router"
      nat_name    = "mindclade-prod-central-nat"
      subnet_keys = ["prod-central"]
    }
  }
}
```

The module deletes the provider-created default route and recreates it as a
Terraform-owned, deletion-protected resource. Set
`create_default_internet_route = false` only for a PSC-only or otherwise
explicitly routed network; Public Cloud NAT is then rejected. The route alone
does not grant an external IP or NAT access.

Firewall policies/rules, Shared VPC host and service-project attachment, PSC,
hybrid connectivity, IPv6, DNS policy, and IPAM allocation authority remain
separate lifecycle boundaries. Callers must validate CIDR overlap, egress policy,
quota, NAT capacity, and connectivity at representative scale. Destruction
requires an explicit code change removing both provider and Terraform guards.
This repository module is a baseline, not evidence of a deployed or qualified
production network.
