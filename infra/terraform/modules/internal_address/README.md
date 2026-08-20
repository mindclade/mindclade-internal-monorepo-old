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

<!-- BEGIN_TF_DOCS -->
## Requirements

| Name | Version |
| ---- | ------- |
| <a name="requirement_terraform"></a> [terraform](#requirement\_terraform) | >= 1.9.0, < 2.0.0 |
| <a name="requirement_google"></a> [google](#requirement\_google) | >= 7.41.0, < 8.0.0 |

## Providers

| Name | Version |
| ---- | ------- |
| <a name="provider_google"></a> [google](#provider\_google) | >= 7.41.0, < 8.0.0 |

## Inputs

| Name | Description | Type | Default | Required |
| ---- | ----------- | ---- | ------- | :------: |
| <a name="input_addresses"></a> [addresses](#input\_addresses) | Reserved internal IPv4 addresses, keyed by a stable name — in this estate, the<br/>environment.<br/><br/>The map KEY is not the resource name; `name` is, and it is what a Kubernetes Gateway<br/>refers to in `spec.addresses[].value`. That name is an interface between Terraform and<br/>Argo: Terraform reserves the address, the Gateway names it, and neither generates it.<br/>Renaming one without the other produces a Gateway that never gets an address, and the<br/>only symptom is a Gateway stuck without a programmed status. | <pre>map(object({<br/>    project_id = string<br/>    name       = string<br/>    region     = string<br/><br/>    # Self-link of the subnet to allocate from. It must be a PRIVATE subnet: a<br/>    # REGIONAL_MANAGED_PROXY subnet holds the load balancer's proxies and cannot allocate an<br/>    # address, which is the natural mistake here because both belong to the same load<br/>    # balancer.<br/>    subnetwork = string<br/><br/>    # Omit to let GCP choose. Pinning it is worth doing once a DNS record points at it, since<br/>    # an unpinned address can come back different after a destroy/recreate — and the DNS<br/>    # records in the private zones would then point at nothing.<br/>    address = optional(string)<br/><br/>    description = optional(string, "Reserved internal address managed by Terraform.")<br/>    purpose     = optional(string, "GCE_ENDPOINT")<br/>  }))</pre> | n/a | yes |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_addresses"></a> [addresses](#output\_addresses) | The allocated IPv4 address by key.<br/><br/>This is what the private DNS zones' A records point at. Reading it from here rather than<br/>hardcoding it is what keeps the record and the reservation from drifting apart. |
| <a name="output_names"></a> [names](#output\_names) | The address resource name by key.<br/><br/>THE Terraform↔Argo interface. A GKE Gateway refers to this string in<br/>`spec.addresses[].value` with `type: NamedAddress`; nothing generates it on the Argo side.<br/>Treat it as an interface: renaming it is a coordinated change across both repositories,<br/>and doing it in one alone leaves a Gateway that never receives an address, with no error<br/>beyond a status that never becomes programmed. |
| <a name="output_self_links"></a> [self\_links](#output\_self\_links) | Address self-links by key. |
<!-- END_TF_DOCS -->
