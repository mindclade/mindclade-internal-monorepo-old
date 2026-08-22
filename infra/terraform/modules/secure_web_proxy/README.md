# Secure Web Proxy module

Creates protected, regional Google Cloud Secure Web Proxy gateways for governed provider egress.
Every gateway is an explicit HTTPS proxy with TLS inspection, a bounded exact-host allow rule,
and an explicit terminal deny. The allow rule checks both the CONNECT/session host and the
decrypted HTTP host; an uninspected HTTPS configuration is not expressible through this module.

The caller owns the VPC, private proxy subnetwork, reserved address, gateway frontend
certificate, CA Service hierarchy, TLS inspection policy, workload trust-bundle distribution,
firewall policy, and service enablement. The activation evidence must prove that the Rust
gateway trusts only the reviewed interception CA, that a permitted provider succeeds, and that
an unlisted SNI/HTTP host is rejected. Neither provider tokens nor CA private material are module
inputs.

All policy, rule, and gateway resources use both provider deletion prevention and Terraform
`prevent_destroy`. To retire one, a reviewed change must remove both protections before apply.

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
| <a name="input_proxies"></a> [proxies](#input\_proxies) | TLS-inspecting explicit Secure Web Proxies keyed by stable environment ownership key. | <pre>map(object({<br/>    project_id                = string<br/>    region                    = string<br/>    name                      = string<br/>    scope                     = string<br/>    address                   = string<br/>    network                   = string<br/>    subnetwork                = string<br/>    gateway_certificate_url   = string<br/>    tls_inspection_policy_url = string<br/>    allowed_hosts             = set(string)<br/>  }))</pre> | n/a | yes |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_allowed_hosts"></a> [allowed\_hosts](#output\_allowed\_hosts) | Normalized exact host allowlists keyed by ownership key for release evidence. |
| <a name="output_gateway_ids"></a> [gateway\_ids](#output\_gateway\_ids) | Immutable Secure Web Proxy resource identifiers keyed by ownership key. |
| <a name="output_https_proxy_urls"></a> [https\_proxy\_urls](#output\_https\_proxy\_urls) | HTTPS proxy URLs for workload configuration; certificate trust is a separate secret contract. |
<!-- END_TF_DOCS -->
